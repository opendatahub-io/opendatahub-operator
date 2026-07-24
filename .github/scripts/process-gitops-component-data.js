const { getLatestCommitSha, parseManifestFile, updateManifestFile } = require('./manifest-utils');

/**
 * Process GitOps component data and update manifests
 * Replaces the tracker-parsing logic with structured JSON input
 *
 * Expected input format:
 * {
 *   "dashboard": {
 *     "ref": "v2.25.0",
 *     "display_name": "OpenDataHub Dashboard"
 *   },
 *   "workbenches/notebook-controller": {
 *     "ref": "v1.8.0-rc.1",
 *     "display_name": "Notebook Controller"
 *   },
 *   "workbenches/notebooks": {
 *     "ref": "v2.12.0",
 *     "display_name": "Notebooks"
 *   }
 * }
 */

// Multi-component mapping: one input component updates multiple manifest entries
const MULTI_COMPONENT_MAPPING = {
    'workbenches/notebook-controller': [
        'workbenches/kf-notebook-controller',
        'workbenches/odh-notebook-controller'
    ]
};


module.exports = async ({ github, core, componentData }) => {
    try {

        // Parse JSON input
        let parsedData;
        try {
            parsedData = JSON.parse(componentData || '{}');
        } catch (error) {
            core.setFailed(`Failed to parse component data JSON: ${error.message}`);
            return;
        }


        // Process components
        const updates = [];
        if (Object.keys(parsedData).length > 0) {

            // Read existing manifest data
            const manifestFile = 'get_all_manifests.sh';
            const manifestComponents = parseManifestFile(manifestFile);

            // Create a lookup map for existing components (ODH only)
            const allComponents = manifestComponents.odh;
            const componentLookup = new Map();
            for (const comp of allComponents) {
                componentLookup.set(comp.componentName, comp);
            }

            // Process each component update
            for (const [componentName, componentData] of Object.entries(parsedData)) {
                const newRef = componentData.ref.trim();
                console.log(`Processing ${componentName} → ${newRef}`);

                // Check if this is a multi-component mapping (e.g., workbenches/notebook-controller)
                const targetComponents = MULTI_COMPONENT_MAPPING[componentName] || [componentName];

                let resolvedCommitSha = null;

                for (const targetComponent of targetComponents) {
                    // Find existing component info
                    const existingComponent = componentLookup.get(targetComponent);
                    if (!existingComponent) {
                        console.log(`Warning: Component "${targetComponent}" not found in manifest file`);
                        continue;
                    }

                    // Resolve commit SHA only once (all targets should use same repo)
                    if (!resolvedCommitSha) {
                        resolvedCommitSha = await getLatestCommitSha(github, existingComponent.org, existingComponent.repo, newRef);

                        if (!resolvedCommitSha) {
                            console.log(`Failed to resolve commit SHA for ${componentName}:${newRef}`);
                            break; // Skip all targets if SHA resolution fails
                        }

                    }

                    // Generate final ref with SHA
                    const finalRef = `${newRef}@${resolvedCommitSha}`;

                    console.log(`Updating ${targetComponent}`);

                    updates.push({
                        componentName: existingComponent.componentName,
                        org: existingComponent.org,
                        repo: existingComponent.repo,
                        newRef: finalRef,
                        sourcePath: existingComponent.sourcePath,
                        originalLine: existingComponent.originalLine,
                        logMessage: `Updated ${existingComponent.platform}:${targetComponent} to ${newRef}`
                    });
                }
            }

            // Apply updates to manifest file
            if (updates.length > 0) {
                updateManifestFile(manifestFile, updates);
            }
        }

    } catch (error) {
        console.error('Error processing GitOps component data:', error);
        core.setFailed(`GitOps component data processing failed: ${error.message}`);
    }
};
