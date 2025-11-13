const { parseManifestFile, updateManifestFile } = require('./manifest-utils');

/**
 * Update component manifest references in get_all_manifests.sh
 * Reads environment variables exported by get-release-branches.js
 */

module.exports = () => {
    const manifestFile = 'get_all_manifests.sh';

    console.log('Updating component branches/tags...');

    const manifestComponents = parseManifestFile(manifestFile);

    const specPrefix = 'component_spec_';

    const updates = new Map();

    for (const [key, value] of Object.entries(process.env)) {
        if (!key.startsWith(specPrefix)) {
            continue;
        }

        const componentKey = key.substring(specPrefix.length);
        const shaKey = `component_sha_${componentKey}`;
        const shaValue = process.env[shaKey] || '';
        const orgKey = `component_org_${componentKey}`;
        const orgValue = process.env[orgKey] || '';

        const newRef = shaValue ? `${value}@${shaValue}` : value;

        let found = false;
        for (const [manifestComponentName, componentInfo] of manifestComponents) {
            // Normalize both to dashes for comparison
            // get-release-branches.js uses: "/" -> "-", so we normalize everything to "-"
            const normalizedManifest = manifestComponentName.toLowerCase().replace(/[\/\-_]/g, '-');
            const normalizedKey = componentKey.toLowerCase().replace(/[\/\-_]/g, '-');

            // Also try without workbenches prefix for special notebook-controller case
            const normalizedManifestWithoutPrefix = manifestComponentName.toLowerCase()
                .replace(/^workbenches[\/\-]/, '')
                .replace(/[\/\-_]/g, '-');

            if (normalizedManifest === normalizedKey ||
                normalizedManifestWithoutPrefix === normalizedKey) {
                const displayRef = shaValue ? `${value}@${shaValue.substring(0, 8)}` : value;

                updates.set(manifestComponentName, {
                    org: orgValue || componentInfo.org,
                    repo: componentInfo.repo,
                    newRef: newRef,
                    sourcePath: componentInfo.sourcePath,
                    originalLine: componentInfo.originalLine,
                    logMessage: `Updated ${manifestComponentName} to ${displayRef}`
                });

                console.log(`  Updating ${manifestComponentName} to: ${displayRef}`);
                found = true;
                break;
            }
        }

        if (!found) {
            console.log(`  ⚠️  Warning: No matching component found for env var ${key}`);
        }
    }

    updateManifestFile(manifestFile, updates);
};
