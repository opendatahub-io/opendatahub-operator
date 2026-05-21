# Auth Security Migration Guide

## Overview

This guide covers the migration from `system:authenticated` group usage to explicit, secure group configurations in the OpenDataHub/RHOAI Auth custom resource. This change follows Kubernetes security best practices and eliminates security vulnerabilities.

## Background

### Security Issue

The `system:authenticated` group grants access to **any authenticated user** in the cluster, which violates Kubernetes security best practices:

- **Kubernetes Documentation**: "Review bindings for the `system:authenticated` group and remove them where possible"
- **Google Cloud Best Practices**: "In practice, this isn't meaningfully different from `system:unauthenticated` because anyone can create a Google Account"

### What Changed

1. **API Validation**: New Auth CRs can no longer contain `system:authenticated` in AdminGroups or AllowedGroups
2. **Default Behavior**: Default Auth CRs now use platform-specific groups instead of `system:authenticated`
3. **Controller Warnings**: Existing usage triggers warnings and status conditions

## Migration Path

### Phase 1: Detection (Current Release)

**Check for existing usage:**
```bash
# Check Auth CRs for system:authenticated usage
oc get auth -o yaml | grep -A 10 -B 10 "system:authenticated"

# Get deprecation status from Auth CR
oc describe auth auth
```

**Expected output if deprecated usage exists:**
```
Conditions:
  Type:    DeprecatedGroupUsage
  Status:  True
  Reason:  SystemAuthenticatedDeprecated
  Message: Deprecated usage detected: AllowedGroups contains deprecated 'system:authenticated' group
```

### Phase 2: Migration (Required)

#### Automatic Migration Script

Create and run the migration script:

```bash
#!/bin/bash
# migrate-auth-security.sh

echo "🔍 Checking for Auth CR with deprecated system:authenticated usage..."

# Get current Auth CR
CURRENT_AUTH=$(oc get auth auth -o json 2>/dev/null)
if [ $? -ne 0 ]; then
    echo "❌ No Auth CR found named 'auth'"
    exit 1
fi

# Check if system:authenticated is used
ADMIN_GROUPS_CHECK=$(echo "$CURRENT_AUTH" | jq -r '.spec.adminGroups[]? // empty' | grep "system:authenticated" || true)
ALLOWED_GROUPS_CHECK=$(echo "$CURRENT_AUTH" | jq -r '.spec.allowedGroups[]? // empty' | grep "system:authenticated" || true)

if [ -z "$ADMIN_GROUPS_CHECK" ] && [ -z "$ALLOWED_GROUPS_CHECK" ]; then
    echo "✅ No deprecated system:authenticated usage found"
    exit 0
fi

echo "⚠️ Found deprecated system:authenticated usage"

# Determine platform-specific replacement groups
PLATFORM=$(oc get infrastructure cluster -o jsonpath='{.status.platform}' 2>/dev/null || echo "unknown")
case $PLATFORM in
    "OpenShift")
        NEW_ADMIN_GROUP="rhods-admins"
        NEW_ALLOWED_GROUP="rhods-users"
        ;;
    *)
        NEW_ADMIN_GROUP="odh-admins"
        NEW_ALLOWED_GROUP="odh-users"
        ;;
esac

echo "🔄 Migrating to platform-specific groups:"
echo "   Admin group: $NEW_ADMIN_GROUP"
echo "   Allowed group: $NEW_ALLOWED_GROUP"

# Create backup
echo "$CURRENT_AUTH" > auth-backup-$(date +%Y%m%d-%H%M%S).json
echo "💾 Backup created: auth-backup-$(date +%Y%m%d-%H%M%S).json"

# Update Auth CR
echo "$CURRENT_AUTH" | jq --arg admin_group "$NEW_ADMIN_GROUP" --arg allowed_group "$NEW_ALLOWED_GROUP" '
    .spec.adminGroups = [.spec.adminGroups[] | if . == "system:authenticated" then $admin_group else . end] |
    .spec.allowedGroups = [.spec.allowedGroups[] | if . == "system:authenticated" then $allowed_group else . end] |
    .metadata.annotations["auth.opendatahub.io/migration-timestamp"] = now | todate
' | oc apply -f -

if [ $? -eq 0 ]; then
    echo "✅ Auth CR successfully migrated"
    echo "🔍 Verifying migration..."
    sleep 2
    oc describe auth auth | grep -A 5 "Conditions:"
else
    echo "❌ Migration failed"
    exit 1
fi

echo "📋 Next steps:"
echo "1. Verify users can still access the platform"
echo "2. Ensure the groups '$NEW_ADMIN_GROUP' and '$NEW_ALLOWED_GROUP' exist and contain appropriate users"
echo "3. Remove any manual RoleBindings that reference system:authenticated"
```

#### Manual Migration Steps

1. **Backup existing Auth CR:**
   ```bash
   oc get auth auth -o yaml > auth-backup.yaml
   ```

2. **Identify replacement groups:**
   - **OpenDataHub**: `odh-admins`, `odh-users`
   - **RHOAI Self-Managed**: `rhods-admins`, `rhods-users`
   - **RHOAI Managed**: `dedicated-admins`, `rhods-users`

3. **Update Auth CR:**
   ```bash
   # Edit the Auth CR
   oc edit auth auth

   # Replace system:authenticated with appropriate groups
   spec:
     adminGroups:
     - odh-admins  # or rhods-admins for RHOAI
     allowedGroups:
     - odh-users   # or rhods-users for RHOAI
   ```

4. **Verify migration:**
   ```bash
   # Check that deprecated conditions are removed
   oc describe auth auth

   # Verify user access still works
   oc auth can-i get auth --as=system:serviceaccount:default:test
   ```

### Phase 3: Validation

After migration, verify:

1. **No deprecation warnings in logs:**
   ```bash
   oc logs -n opendatahub-operator-system deployment/opendatahub-operator-controller-manager | grep -i "system:authenticated"
   ```

2. **Status conditions are clean:**
   ```bash
   oc get auth auth -o jsonpath='{.status.conditions[?(@.type=="DeprecatedGroupUsage")]}'
   ```

3. **Users retain appropriate access:**
   ```bash
   # Test admin user access
   oc auth can-i create auth --as=user:admin-user

   # Test regular user access
   oc auth can-i get auth --as=user:regular-user
   ```

## Recommended Group Structure

### Platform-Specific Groups

| Platform | Admin Group | User Group | Purpose |
|----------|-------------|------------|---------|
| OpenDataHub | `odh-admins` | `odh-users` | Full platform admin / General user access |
| RHOAI Self-Managed | `rhods-admins` | `rhods-users` | Full platform admin / General user access |
| RHOAI Managed | `dedicated-admins` | `rhods-users` | Cluster admin / Data science users |

### Custom Group Examples

For organizations with specific needs:

```yaml
spec:
  adminGroups:
    - "data-science-platform-admins"
    - "ml-ops-team"
  allowedGroups:
    - "data-scientists"
    - "ml-engineers"
    - "business-analysts"
```

## Troubleshooting

### Common Issues

#### Issue: "Users can't access the platform after migration"
**Solution:**
1. Check that the new groups exist:
   ```bash
   oc get groups
   ```
2. Verify users are in the correct groups:
   ```bash
   oc get group odh-users -o yaml
   ```
3. Check RoleBindings are created:
   ```bash
   oc get rolebinding -A | grep data-science
   oc get clusterrolebinding | grep data-science
   ```

#### Issue: "API validation rejects Auth CR updates"
**Solution:**
This means the CR contains `system:authenticated`. Use the migration script or manually edit to remove it:
```bash
# Find the problematic field
oc get auth auth -o yaml | grep -n "system:authenticated"

# Edit to replace with appropriate groups
oc edit auth auth
```

#### Issue: "Controller logs show security warnings"
**Solution:**
This indicates an existing Auth CR still contains `system:authenticated`. Follow the migration steps above.

### Rollback Procedure

If migration causes issues:

1. **Restore from backup:**
   ```bash
   oc apply -f auth-backup.yaml
   ```

2. **Check operator status:**
   ```bash
   oc get pods -n opendatahub-operator-system
   oc logs -n opendatahub-operator-system deployment/opendatahub-operator-controller-manager
   ```

3. **Verify user access restored:**
   ```bash
   oc auth can-i get auth --as=user:test-user
   ```

## Security Benefits

After migration:

- ✅ **Explicit Access Control**: Only users in designated groups have access
- ✅ **Audit Trail**: Clear mapping of users to groups to permissions
- ✅ **Principle of Least Privilege**: No overly broad access grants
- ✅ **Kubernetes Compliance**: Follows official security recommendations
- ✅ **Defense in Depth**: Multiple layers of validation prevent misconfigurations

## References

- [Kubernetes RBAC Good Practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- [Google Cloud RBAC Best Practices](https://cloud.google.com/kubernetes-engine/docs/best-practices/rbac)
- [OpenShift Authentication and Authorization](https://docs.openshift.com/container-platform/latest/authentication/index.html)

## Support

For questions or issues:
1. Check the [troubleshooting section](#troubleshooting) above
2. Review Auth CR status: `oc describe auth auth`
3. Check operator logs for detailed error messages
4. File an issue with the complete migration script output and error details