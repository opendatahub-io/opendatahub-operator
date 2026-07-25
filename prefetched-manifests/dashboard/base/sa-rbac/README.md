# RBAC - Service Account (SA)

If you're adding permissions here -- you're likely in the wrong.

High level notes:
- You have an explicit `backend` folder need or your package has an explicit `bff` need
  - If you're in frontend code -- you don't need to be here
- Users are unable to do your rule themselves (hint: they almost always can)
  - seek out `/manifests/common/rbac/all-users` folder instead

If you're in need of adding more files or more rules in this folder, make sure you get a signoff from a Dashboard Staff+ eng.
