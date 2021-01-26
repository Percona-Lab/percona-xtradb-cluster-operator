package collector

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"

	"github.com/percona/percona-xtradb-cluster-operator/cmd/pitr/pxc"
	"github.com/percona/percona-xtradb-cluster-operator/cmd/pitr/storage"
)

type Collector struct {
	db             *pxc.PXC
	storage        storage.Storage
	lastSet        string // last uploaded binary logs set
	pxcServiceName string // k8s service name for PXC, its for get correct host for connection
	pxcUser        string // user for connection to PXC
	pxcPass        string // password for connection to PXC
}

type Config struct {
	PXCServiceName string `env:"PXC_SERVICE,required"`
	PXCUser        string `env:"PXC_USER,required"`
	PXCPass        string `env:"PXC_PASS,required"`
	S3Endpoint     string `env:"ENDPOINT" envDefault:"s3.amazonaws.com"`
	S3AccessKeyID  string `env:"ACCESS_KEY_ID,required"`
	S3AccessKey    string `env:"SECRET_ACCESS_KEY,required"`
	S3BucketURL    string `env:"S3_BUCKET_URL,required"`
	S3Region       string `env:"DEFAULT_REGION,required"`
	BufferSize     int64  `env:"BUFFER_SIZE"`
	CollectSpanSec int64  `env:"COLLECT_SPAN_SEC" envDefault:"60"`
}

const (
	lastSetFileName string = "last-binlog-set" // name for object where the last binlog set will stored
	gtidPostfix     string = "-gtid-set"       // filename postfix for files with GTID set
)

func New(c Config) (*Collector, error) {
	bucketArr := strings.Split(c.S3BucketURL, "/")
	prefix := ""
	// if c.S3BucketURL looks like "my-bucket/data/more-data" we need prefix to be "data/more-data/"
	if len(bucketArr) > 1 {
		prefix = strings.TrimPrefix(c.S3BucketURL, bucketArr[0]+"/") + "/"
	}
	s3, err := storage.NewS3(strings.TrimPrefix(strings.TrimPrefix(c.S3Endpoint, "https://"), "http://"), c.S3AccessKeyID, c.S3AccessKey, bucketArr[0], prefix, c.S3Region, strings.HasPrefix(c.S3Endpoint, "https"))
	if err != nil {
		return nil, errors.Wrap(err, "new storage manager")
	}

	// get last binlog set stored on S3
	lastSetObject, err := s3.GetObject(lastSetFileName)
	if err != nil {
		return nil, errors.Wrap(err, "get last set content")
	}
	lastSet, err := ioutil.ReadAll(lastSetObject)
	if err != nil && minio.ToErrorResponse(errors.Cause(err)).Code != "NoSuchKey" {
		return nil, errors.Wrap(err, "read last gtid set")
	}

	return &Collector{
		storage:        s3,
		lastSet:        string(lastSet),
		pxcUser:        c.PXCUser,
		pxcServiceName: c.PXCServiceName,
	}, nil
}

func (c *Collector) Run() error {
	err := c.newDB()
	if err != nil {
		return errors.Wrap(err, "new db connection")
	}
	defer c.close()

	err = c.CollectBinLogs()
	if err != nil {
		return errors.Wrap(err, "collect binlog files")
	}

	return nil
}

func (c *Collector) newDB() error {
	host, err := pxc.GetPXCLastHost(c.pxcServiceName)
	if err != nil {
		return errors.Wrap(err, "get host")
	}

	file, err := os.Open("/etc/mysql/mysql-users-secret/xtrabackup")
	if err != nil {
		return errors.Wrap(err, "open file")
	}
	pxcPass, err := ioutil.ReadAll(file)
	if err != nil {
		return errors.Wrap(err, "read password")
	}
	c.pxcPass = string(pxcPass)

	log.Println("Reading binlogs from pxc with hostname=", host)

	c.db, err = pxc.NewPXC(host, c.pxcUser, c.pxcPass)
	if err != nil {
		return errors.Wrapf(err, "new manager with host %s", host)
	}

	return nil
}

func (c *Collector) close() error {
	return c.db.Close()
}

func (c *Collector) CollectBinLogs() error {
	list, err := c.db.GetBinLogList()
	if err != nil {
		return errors.Wrap(err, "get binlog list")
	}

	// get last uploaded binlog file name
	lastUploadedBinlogName, err := c.db.GetBinLogName(c.lastSet)
	if err != nil {
		return errors.Wrap(err, "get latst uploaded binlog name by set")
	}

	upload := false
	// if there are no uploaded files we going to upload every binlog file
	if len(lastUploadedBinlogName) == 0 {
		upload = true
	}

	for _, binlog := range list {
		binlogSet := ""
		// this check is for uploading starting from needed file
		if binlog.Name == lastUploadedBinlogName {
			binlogSet, err = c.db.GetGTIDSet(binlog.Name)
			if err != nil {
				return errors.Wrap(err, "get binlog gtid set")
			}
			if c.lastSet != binlogSet {
				upload = true
			}
		}
		if upload {
			err = c.manageBinlog(binlog)
			if err != nil {
				return errors.Wrap(err, "manage binlog")
			}
		}
		// need this for start uploading files that goes after current
		if c.lastSet == binlogSet {
			upload = true
		}
	}

	return nil
}

func mergeErrors(a, b error) error {
	if a != nil && b != nil {
		return errors.New(a.Error() + "; " + b.Error())
	}
	if a != nil {
		return a
	}

	return b
}

func (c *Collector) manageBinlog(binlog pxc.Binlog) (err error) {
	set, err := c.db.GetGTIDSet(binlog.Name)
	if err != nil {
		return errors.Wrap(err, "get GTID set")
	}
	// if binlog don't have gtid we do not upload it
	// because it's empty and don't have any information
	if len(set) == 0 {
		return nil
	}

	binlogTmstmp, err := c.db.GetBinLogFirstTimestamp(binlog.Name)
	if err != nil {
		return errors.Wrapf(err, "get first timestamp for %s", binlog.Name)
	}

	binlogName := fmt.Sprintf("binlog_%s_%x", binlogTmstmp, md5.Sum([]byte(set)))

	var setBuffer bytes.Buffer
	setBuffer.WriteString(set)

	tmpDir := os.TempDir() + "/"

	err = os.Remove(tmpDir + binlog.Name)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "remove temp file")
	}

	err = syscall.Mkfifo(tmpDir+binlog.Name, 0666)
	if err != nil {
		return errors.Wrap(err, "make named pipe file error")
	}

	errBuf := &bytes.Buffer{}
	cmd := exec.Command("mysqlbinlog", "-R", "--raw", "-h"+c.db.GetHost(), "-u"+c.pxcUser, binlog.Name)
	cmd.Env = append(cmd.Env, "MYSQL_PWD="+c.pxcPass)
	cmd.Dir = os.TempDir()
	cmd.Stderr = errBuf

	err = cmd.Start()
	if err != nil {
		return errors.Wrap(err, "run mysqlbinlog command")
	}

	log.Println("Starting to process binlog with name", binlog.Name)

	file, err := os.OpenFile(tmpDir+binlog.Name, os.O_RDONLY, os.ModeNamedPipe)
	if err != nil {
		return errors.Wrap(err, "open named pipe file error")
	}

	defer func() {
		errC := file.Close()
		if errC != nil {
			err = mergeErrors(err, errors.Wrapf(errC, "close tmp file for %s", binlog.Name))
			return
		}
		errR := os.Remove(tmpDir + binlog.Name)
		if errR != nil {
			err = mergeErrors(err, errors.Wrapf(errR, "remove tmp file for %s", binlog.Name))
			return
		}
	}()

	// create pipe to transfer data from binlog pipe to s3
	pr, pw := io.Pipe()

	go readBinlog(file, pw, errBuf, binlog.Name)

	err = c.storage.PutObject(binlogName, pr, -1)
	if err != nil {
		return errors.Wrapf(err, "put %s object", binlog.Name)
	}

	log.Println("Successfully writen binlog to s3 with name", binlog.Name)

	err = cmd.Wait()
	if err != nil {
		return errors.Wrap(err, "wait mysqlbinlog command")
	}

	err = c.storage.PutObject(binlogName+gtidPostfix, &setBuffer, int64(setBuffer.Len()))
	if err != nil {
		return errors.Wrap(err, "put gtid-set object")
	}

	setBuffer.WriteString(set)
	err = c.storage.PutObject(lastSetFileName, &setBuffer, int64(setBuffer.Len()))
	if err != nil {
		return errors.Wrap(err, "put last-set object")
	}
	c.lastSet = set

	return nil
}

func readBinlog(file *os.File, pipe *io.PipeWriter, errBuf *bytes.Buffer, binlogName string) {
	b := make([]byte, 10485760) //alloc buffer for 10mb

	// we need this because if binlog slow and didn't write anything
	// to file yet, we have to skip this error and try to read again
	// untill some data appears
	isEmpty := true
	for {
		if errBuf.Len() != 0 {
			// stop reading since we receive error from binlog command in stderr
			pipe.CloseWithError(errors.Errorf("Error: mysqlbinlog %s", errBuf.String()))
			return
		}
		n, err := file.Read(b)
		if err == io.EOF {
			// if we receive EOF immediatly after starting to read file
			// we have to skip it, since data is not appeared yet
			// if we got something already and receive EOF error - then exit
			if isEmpty {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			break
		}
		if err != nil && !strings.Contains(err.Error(), "file already closed") {
			pipe.CloseWithError(errors.Wrapf(err, "Error: reading named pipe for %s", binlogName))
			return
		}
		if n == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		pipe.Write(b[:n])
		isEmpty = false
	}
	// we need to close pipe with io.EOF because it's common way
	// to indicate that we reached file end and it's not a error
	pipe.CloseWithError(io.EOF)
}
