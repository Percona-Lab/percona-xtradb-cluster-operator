package perconaxtradbbackup

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/percona/percona-xtradb-cluster-operator/clientcmd"
	api "github.com/percona/percona-xtradb-cluster-operator/pkg/apis/pxc/v1alpha1"
)

// SelectNode returns appropriate pxc-node for make a backup from
func (r *ReconcilePerconaXtraDBBackup) SelectNode(cr *api.PerconaXtraDBBackup) (string, error) {
	proxysqlList := corev1.PodList{}
	err := r.client.List(context.TODO(),
		&client.ListOptions{
			Namespace: cr.Namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app.kubernetes.io/name":       "percona-xtradb-cluster",
				"app.kubernetes.io/instance":   cr.Spec.PXCCluster,
				"app.kubernetes.io/component":  "proxysql",
				"app.kubernetes.io/managed-by": "percona-xtradb-cluster-operator",
				"app.kubernetes.io/part-of":    "percona-xtradb-cluster",
			}),
		},
		&proxysqlList,
	)
	if err != nil {
		return "", fmt.Errorf("get proxysql list: %v", err)
	}

	var proxyPod *corev1.Pod
	for _, proxysql := range proxysqlList.Items {
		if proxysql.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, cstate := range proxysql.Status.ContainerStatuses {
			if cstate.Name == "proxysql" && cstate.Ready {
				proxyPod = &proxysql
				break
			}
		}
	}

	rwNodeIP := ""
	if proxyPod != nil {
		cl, err := clientcmd.NewClient()
		if err != nil {
			return "", fmt.Errorf("create new k8s client: %v", err)
		}
		var outb, errb bytes.Buffer
		err = cl.Exec(
			proxyPod,
			"proxysql",
			[]string{"bash", "-c", `mysql -sN -h127.0.0.1 -P6032 -u$PROXY_ADMIN_USER -p$PROXY_ADMIN_PASSWORD -e "SELECT hostname FROM runtime_mysql_servers WHERE hostgroup_id=11 AND status='ONLINE';"`},
			nil,
			&outb,
			&errb,
			false,
		)
		if err != nil {
			return "", fmt.Errorf("define write pod: %v / exec: %s", err, errb.String())
		}

		rwNodeIP = strings.TrimSpace(outb.String())
	}

	pxcnodesList := corev1.PodList{}
	err = r.client.List(context.TODO(),
		&client.ListOptions{
			Namespace: cr.Namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app.kubernetes.io/name":       "percona-xtradb-cluster",
				"app.kubernetes.io/instance":   cr.Spec.PXCCluster,
				"app.kubernetes.io/component":  "pxc",
				"app.kubernetes.io/managed-by": "percona-xtradb-cluster-operator",
				"app.kubernetes.io/part-of":    "percona-xtradb-cluster",
			}),
		},
		&pxcnodesList,
	)
	if err != nil {
		return "", fmt.Errorf("get pxc nodes list: %v", err)
	}

	bcpNode := ""
	for _, node := range pxcnodesList.Items {
		if node.Status.PodIP == rwNodeIP || node.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, cstate := range node.Status.ContainerStatuses {
			if cstate.Name == "node" && cstate.Ready {
				bcpNode = node.Status.PodIP
				break
			}
		}
	}

	return bcpNode, nil
}
