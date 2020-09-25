.. _debug-images:

Debug
=================

For the cases when Pods are failing for some reason or just show abnormal
behavior, the Operator can be used with a special *debug images*. Percona XtraDB
Cluster debug image has the following specifics:

* it avoids restarting on fail,
* it contains additional tools useful for debugging (sudo, telnet, gdb, etc.),
* it has debug mode enabled for the logs.

There are debug versions for all :ref:`Percona certified images<custom-registry-images>`: they have same names as normal images with a special ``-debug`` suffix in their version tag: for example, ``percona-xtradb-cluster-operator:{{{release}}}-debug``.

Particularly, using such image is useful if the container entry point fails
(e.g. ``mysqld`` crashes). In such a situation, Pod is continuously restarting.
Continuous restarts prevent to get console access to the container,
and so a special approach is needed to make fixes.

To use the debug image instead of the normal one, find the needed image name
:ref:`in the list of certified images<custom-registry-images>` and set it
for the proper key in the ``deploy/cr.yaml`` configuration file. For example,
set the following value of the ``pxc.image`` key to use the Percona XtraDB
Cluster debug image:

* ``percona/percona-xtradb-cluster:{{{pxc80recommended}}}-debug`` for Percona XtraDB Cluster 8.0,
* ``percona/percona-xtradb-cluster:{{{pxc57recommended}}}-debug`` for Percona XtraDB Cluster 5.7.

The Pod should be restarted to get the new image.

.. note::  When the Pod is continuously restarting, you may have to delete it
   to apply image changes.
