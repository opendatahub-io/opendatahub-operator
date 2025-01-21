# Permissions For Users

When adding permissions for users, it's best to consider the "[reasonable level](#reasonable-level)" of permissions.

## Reasonable Level

When granting access to a bot (like a service account) you'd aim to grant specific actions the bot would directly do -- need patch? give patch; need read? give read. However, when granting permissions to users, you often want to take a wider approach -- need patch? give update & patch; need read? give read, list, and watch.

The CRUD actions an admin or allowed user gets, shouldn't *just* be specific to what our code / platform does. Consider the k8s permissions related to each CRUD action and the subsequent k8s verbs:
* C - Create
* R - Get, List, Watch
* U - Update, Patch
* D - Delete

Furthermore, when a user gets “C” (create) actions... they sorta should be able to delete them to clean up their mistakes. Naturally they’ll need read permissions too, otherwise how can they interact with them? The mapping looks something like this:
* Need **C** => Gets **CRUD**
* Need **R** => Gets **R**
* Need **U** => Gets **RU**
* Need **D** => Gets **CRUD**

If you need one of the CRUD actions, you get the corresponding CRUD overlap. It’s all additive. If you need just “R” – you only get to read (which boils down to get, list, & watch verbs). If you need Create? It doesn’t matter what else you need, as “C” gets you the full CRUD.
