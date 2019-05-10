Users
=====

As it is written in the installation part, the operator requires
Kubernetes Secrets to be deployed before it is started. The name of the
required secrets can be set in ``deploy/cr.yaml`` under the
``spec.secrets`` section.

Unprivileged users
------------------

There are no unprivileged (general purpose) user accounts created by
default. If you need general purpose users, please run commands below:

.. code:: bash

   $ kubectl run -it --rm percona-client --image=percona:5.7 --restart=Never -- mysql -hcluster1-pxc-nodes -uroot -proot_password
   mysql> GRANT ALL PRIVILEGES ON database1.* TO 'user1'@'%' IDENTIFIED BY 'password1';

Sync users on the ProxySQL node:

.. code:: bash

   $ kubectl exec -it some-name-pxc-proxysql-0 -- proxysql-admin --config-file=/etc/proxysql-admin.cnf --syncusers

Now check the newly created user. If everything is Ok with it, the
following command will let you successfully login to MySQL shell via
ProxySQL:

.. code:: bash

   $ kubectl run -it --rm percona-client --image=percona:5.7 --restart=Never -- bash -il
   percona-client:/$ mysql -h cluster1-pxc-proxysql -uuser1 -ppassword1
   mysql> SELECT * FROM database1.table1 LIMIT 1;

You may also try executing any simple SQL statement to make sure
permissions have been successfully granted.

System Users
------------

The Operator requires system-level PXC users to automate the PXC
deployment.

To make development and testing easier, ``deploy/secrets.yaml`` secrets
file contains default passwords for PXC system users and are mapped with
key/value pairs. The username is the key and the value is an encoded
password used to access a server or a system object.

You can decode the value in ``secrets.yaml`` with the following command:

.. code:: bash

   echo <value> | base64 --decode

**Warning:** *These users should be used for demonstration and
proof-of-concept purposes only. Do not use the listed PXC user passwords
in production or to run an application!*

+-------------+------------------+------------------------------------+
| User name   | Unencoded        | Description                        |
|             | password         |                                    |
+=============+==================+====================================+
| root        | ``root_password` | Admin - Database administrator.    |
|             | `                | Should be used only for            |
|             |                  | maintenance tasks                  |
+-------------+------------------+------------------------------------+
| xtrabackup  | ``backup_passwor | Backup - `User able to run         |
|             | d``              | backups <https://www.percona.com/d |
|             |                  | oc/percona-xtrabackup/2.4/using_xt |
|             |                  | rabackup/privileges.html>`__       |
+-------------+------------------+------------------------------------+
| monitor     | ``monitor``      | PMM Client User - `User for PMM    |
|             |                  | agent <https://percona.github.io/p |
|             |                  | ercona-xtradb-cluster-operator/con |
|             |                  | figure/users>`__                   |
+-------------+------------------+------------------------------------+
| clusterchec | ``custercheckpas | Cluster Check - `User for liveness |
| k           | sword``          | and readiness                      |
|             |                  | checks <http://galeracluster.com/d |
|             |                  | ocumentation-webpages/monitoringth |
|             |                  | ecluster.html>`__                  |
+-------------+------------------+------------------------------------+
| proxyadmin  | ``admin_password | ProxySQL Admin - administrator who |
|             | ``               | can be used `for adding general    |
|             |                  | purpose ProxySQL                   |
|             |                  | users <https://github.com/sysown/p |
|             |                  | roxysql/wiki/Users-configuration#c |
|             |                  | reating-a-new-user>`__             |
+-------------+------------------+------------------------------------+
| pmmserver   | ``pmmserver``    | PMM Server - `User able to access  |
|             |                  | PMM                                |
|             |                  | Server <https://www.percona.com/do |
|             |                  | c/percona-monitoring-and-managemen |
|             |                  | t/security.html#pmm-security-passw |
|             |                  | ord-protection-enabling>`__        |
+-------------+------------------+------------------------------------+
