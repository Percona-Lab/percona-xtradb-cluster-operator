.. _install-helm:

Install Percona XtraDB Cluster using Helm
=========================================

`Helm <https://github.com/helm/helm>`_ is the package manager for Kubernetes.

Pre-requisites
--------------

Install Helm following its `official installation instructions <https://docs.helm.sh/using_helm/#installing-helm>`_.

.. note:: At least ``2.4.0`` version of Helm is needed to run the following steps.


Installation
-------------

#. Add the Percona's Helm charts repository and make your Helm client up to
   date with it:

   .. code:: bash

      helm repo add percona https://percona.github.io/percona-helm-charts/
      helm repo update

#. Install the Percona XtraDB Cluster Operator:

   .. code:: bash

      helm install my-op percona/pxc-operator

   The ``my-op`` parameter in the above example is the name of `a new release object <https://helm.sh/docs/intro/using_helm/#three-big-concepts>`_ 
   which is created for the Operator when you install its Helm chart. Use any
   arbitrary name with Helm 3.x or omit it with Helm 2.x.

   .. note:: If nothing explicitly specified, ``helm install`` command will work
      with ``default`` namespace. To use different namespace, provide it with
      the following additional parameter: ``--namespace my-namespace``.

#. Install Percona XtraDB Cluster:

   .. code:: bash

      helm install my-db percona/pxc-db

   The ``my-db`` parameter in the above example is the name of `a new release object <https://helm.sh/docs/intro/using_helm/#three-big-concepts>`_ 
   which is created for the Percona XtraDB Cluster when you install its Helm
   chart. Use any arbitrary name with Helm 3.x or omit it with Helm 2.x.

Installing Percona XtraDB Cluster with customized parameters
----------------------------------------------------------------

The command above installs Percona XtraDB Cluster with :ref:`default parameters<operator.custom-resource-options>`.
Custom options can be passed to a ``helm install`` command as a
``--set key=value[,key=value]`` argument. The options passed with a chart can be
any of the Operator's :ref:`operator.custom-resource-options`.

The following example will deploy a Percona XtraDB Cluster Cluster in the
``pxc`` namespace, with disabled backups and 20 Gi storage:

.. code:: bash

   helm install my-db percona/pxc-db --namespace pxc \
     --set pxc.volumeSpec.resources.requests.storage=20Gi \
     --set backup.enabled=false

