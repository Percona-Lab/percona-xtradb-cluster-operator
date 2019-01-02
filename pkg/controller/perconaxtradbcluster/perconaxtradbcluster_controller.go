package perconaxtradbcluster

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	api "github.com/Percona-Lab/percona-xtradb-cluster-operator/pkg/apis/pxc/v1alpha1"
	"github.com/Percona-Lab/percona-xtradb-cluster-operator/pkg/pxc"
	"github.com/Percona-Lab/percona-xtradb-cluster-operator/pkg/pxc/app/statefulset"
	"github.com/Percona-Lab/percona-xtradb-cluster-operator/version"
)

var log = logf.Log.WithName("controller_perconaxtradbcluster")

// Add creates a new PerconaXtraDBCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr)
	if err != nil {
		return err
	}

	return add(mgr, r)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) (reconcile.Reconciler, error) {
	sv, err := version.Server()
	if err != nil {
		return nil, fmt.Errorf("get version: %v", err)
	}

	return &ReconcilePerconaXtraDBCluster{
		client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		serverVersion: sv,
	}, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("perconaxtradbcluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource PerconaXtraDBCluster
	err = c.Watch(&source.Kind{Type: &api.PerconaXtraDBCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcilePerconaXtraDBCluster{}

// ReconcilePerconaXtraDBCluster reconciles a PerconaXtraDBCluster object
type ReconcilePerconaXtraDBCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme

	serverVersion *api.ServerVersion
}

// Reconcile reads that state of the cluster for a PerconaXtraDBCluster object and makes changes based on the state read
// and what is in the PerconaXtraDBCluster.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePerconaXtraDBCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling PerconaXtraDBCluster")

	rr := reconcile.Result{
		RequeueAfter: time.Second * 5,
	}
	// Fetch the PerconaXtraDBCluster instance
	o := &api.PerconaXtraDBCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, o)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			return rr, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if o.ObjectMeta.DeletionTimestamp != nil {
		finalizers := []string{}
		for _, fnlz := range o.GetFinalizers() {
			var sfs api.StatefulApp
			switch fnlz {
			case "delete-proxysql-pvc":
				sfs = statefulset.NewProxy(o)
			case "delete-pxc-pvc":
				sfs = statefulset.NewNode(o)
			}
			// deletePVC is always true on this stage
			// because we never reach this point without finalizers
			err = r.deleteStatfulSet(o.Namespace, sfs, true)
			if err != nil {
				finalizers = append(finalizers, fnlz)
			}
		}

		o.SetFinalizers(finalizers)
		r.client.Update(context.TODO(), o)
		// object is beign deleted, no need in further actions
		return reconcile.Result{}, err
	}

	o.Spec.SetDefaults()

	if o.Spec.PXC == nil {
		return reconcile.Result{}, fmt.Errorf("pxc not specified")
	}

	err = r.deploy(o)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.updatePod(statefulset.NewNode(o), o.Spec.PXC, o)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("pxc upgrade error: %v", err)
	}

	proxysqlSet := statefulset.NewProxy(o)
	if o.Spec.ProxySQL != nil && o.Spec.ProxySQL.Enabled {
		err = r.updatePod(proxysqlSet, o.Spec.ProxySQL, o)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("proxySQL upgrade error: %v", err)
		}
	} else {
		// check if there is need to delete pvc
		deletePVC := false
		for _, fnlz := range o.GetFinalizers() {
			if fnlz == "delete-proxysql-pvc" {
				deletePVC = true
				break
			}
		}

		err = r.deleteStatfulSet(o.Namespace, proxysqlSet, deletePVC)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	err = r.reconcileBackups(o)
	if err != nil {
		return reconcile.Result{}, err
	}

	return rr, nil
}

func (r *ReconcilePerconaXtraDBCluster) deploy(cr *api.PerconaXtraDBCluster) error {
	serverVersion := r.serverVersion
	if cr.Spec.Platform != nil {
		serverVersion.Platform = *cr.Spec.Platform
	}

	nodeSet, err := pxc.StatefulSet(statefulset.NewNode(cr), cr.Spec.PXC, cr, serverVersion)
	if err != nil {
		return err
	}

	err = setControllerReference(cr, nodeSet, r.scheme)
	if err != nil {
		return err
	}

	err = r.client.Create(context.TODO(), nodeSet)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create newStatefulSetNode: %v", err)
	}

	nodesService := pxc.NewServiceNodes(cr)
	err = setControllerReference(cr, nodesService, r.scheme)
	if err != nil {
		return err
	}

	err = r.client.Create(context.TODO(), nodesService)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create PXC Service: %v", err)
	}

	if cr.Spec.ProxySQL != nil && cr.Spec.ProxySQL.Enabled {
		proxySet, err := pxc.StatefulSet(statefulset.NewProxy(cr), cr.Spec.ProxySQL, cr, serverVersion)
		if err != nil {
			return fmt.Errorf("create ProxySQL Service: %v", err)
		}
		err = setControllerReference(cr, proxySet, r.scheme)
		if err != nil {
			return err
		}

		err = r.client.Create(context.TODO(), proxySet)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create newStatefulSetProxySQL: %v", err)
		}

		proxys := pxc.NewServiceProxySQL(cr)
		err = setControllerReference(cr, proxys, r.scheme)
		if err != nil {
			return err
		}

		err = r.client.Create(context.TODO(), proxys)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create PXC Service: %v", err)
		}
	}

	return nil
}

func (r *ReconcilePerconaXtraDBCluster) deleteStatfulSet(namespace string, sfs api.StatefulApp, deletePVC bool) error {
	err := r.client.Delete(context.TODO(), sfs.StatefulSet())
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete proxysql: %v", err)
	}
	if deletePVC {
		err = r.deletePVC(namespace, sfs.Lables())
		if err != nil {
			return fmt.Errorf("delete proxysql pvc: %v", err)
		}
	}

	return nil
}

func (r *ReconcilePerconaXtraDBCluster) deletePVC(namespace string, lbls map[string]string) error {
	list := corev1.PersistentVolumeClaimList{}
	err := r.client.List(context.TODO(),
		&client.ListOptions{
			Namespace:     namespace,
			LabelSelector: labels.SelectorFromSet(lbls),
		},
		&list,
	)
	if err != nil {
		return fmt.Errorf("get list: %v", err)
	}

	for _, pvc := range list.Items {
		err := r.client.Delete(context.TODO(), &pvc)
		if err != nil {
			return fmt.Errorf("delete: %v", err)
		}
	}

	return nil
}

func setControllerReference(cr *api.PerconaXtraDBCluster, obj metav1.Object, scheme *runtime.Scheme) error {
	ownerRef, err := cr.OwnerRef(scheme)
	if err != nil {
		return err
	}
	obj.SetOwnerReferences(append(obj.GetOwnerReferences(), ownerRef))
	return nil
}
