.. _debug-images:

Debug
=================

For the cases when Pods are failing for some reason or just show abnormal behavior, 
the Operator can be used with a special *debug image* of the Percona XtraDB Cluster,
which has the following specifics:

* it avoids restarting on fail,
* it contains additional tools useful for debugging (sudo, telnet, gdb, etc.),
* it has debug mode enabled for the logs.

Particularly, using this image is useful if the container entry point fails
(``mysqld`` crashes). In such a situation, Pod is continuously restarting.
Continuous restarts prevent to get console access to the container,
and so a special approach is needed to make fixes.

To use the debug image instead of the normal one, set the following image name
for the ``pxc.image`` key in the ``deploy/cr.yaml`` configuration file:

* ``percona/percona-xtradb-cluster-operator:{{{release}}}-pxc8.0-debug`` for PXC 8.0,
* ``percona/percona-xtradb-cluster-operator:{{{release}}}-pxc5.7-debug`` for PXC 5.7.

The Pod should be restarted to get the new image.

.. note::  When the Pod is continuously restarting, you may have to delete it
   to apply image changes.
