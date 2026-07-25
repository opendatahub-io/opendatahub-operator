# RBAC - All Users

The goal here is to grant feature-level fetching of resources that are likely in the application namespace.

All role-bindings in this folder should adhere to `system:authenticated` as the subject. If you don't need that, you're in the wrong folder.

Be smart about what you put in here. ClusterRoles are not likely a good idea. If attached to a RoleBinding it mutes the impact a bit. But never lean on cluster__ k8s RBAC constructs.

Almost 100% guaranteed you'll never see a ClusterRoleBinding in this folder. As it's not Dashboard's responsibility to manage those resources.
