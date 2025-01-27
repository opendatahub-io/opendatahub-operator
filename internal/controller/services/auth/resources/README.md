# Permissions For Users

When adding permissions for users, it's best to consider the "[reasonable level](#reasonable-level)" of permissions.

## Reasonable Level

> **Note:** This is a recommendation -- a good starting spot -- not a hard-fast rule.

When granting access to a bot (like a service account) you'd aim to grant specific actions the bot would directly do -- need patch? give patch; need read? give read. However, when granting permissions to users, you often want to take a wider approach -- need patch? give update & patch; need read? give get, list, and watch.

Admins should be considered as people with a desire of a clean user experience, and not a means to an end to complete a development flow. From there, we can look at if it is appropriate for us to be granting such wide permissions.

Consider the CRUD actions an admin or allowed user gets... as a user is granted access over resources, there a logical good UX extension to grant them inverse or complementary access. Consider the following CRUD layout as it is related to each k8s verbs:
* C - Create
* R - Get, List, Watch
* U - Update, Patch
* D - Delete

Furthermore, when a user gets “C” (create) actions... we should consider them having the ability to delete them, as to clean up their mistakes. Naturally they’ll need read permissions too so they can interact with them. The mapping looks something like this:
* Need **C** => Gets **CRUD**
* Need **R** => Gets **R**
* Need **U** => Gets **RU**
* Need **D** => Gets **CRUD**

The guidance would be for you to start with the idea that if you need one of the CRUD actions, you get the corresponding CRUD relationship. It’s all additive. If you need just “R” – you only get to read (which boils down to get, list, & watch powers). If you need Create? You’d look to get the full CRUD gambit.

It’s important to note, this is a guidance and recommendation for the user experience of the admin behind the permissions. Not a hard-fast rule. Consider the ramifications of broadly granting access to the resource in question you’re looking at.
