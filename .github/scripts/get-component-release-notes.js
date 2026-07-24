const { parseManifestFile } = require('./manifest-utils');

// Multi-component mapping: same as process-gitops-component-data.js
const MULTI_COMPONENT_MAPPING = {
    'workbenches/notebook-controller': [
        'workbenches/kf-notebook-controller',
        'workbenches/odh-notebook-controller'
    ]
};

module.exports = async ({ github, core, context }) => {
    const { COMPONENT_DATA: componentDataStr, VERSION: currentTag } = process.env

    try {
        // Parse component data from GitOps input
        let parsedData;
        try {
            parsedData = JSON.parse(componentDataStr || '{}');
        } catch (error) {
            core.setFailed(`Failed to parse component data JSON: ${error.message}`);
            return;
        }

        // Get previous release for comparison
        let previousTag = null;
        try {
            const latestReleaseResult = await github.rest.repos.getLatestRelease({
                owner: context.repo.owner,
                repo: context.repo.repo,
                headers: {
                    'X-GitHub-Api-Version': '2022-11-28',
                    'Accept': 'application/vnd.github+json',
                }
            })
            previousTag = latestReleaseResult.data["tag_name"]
        } catch (error) {
            console.log('No previous release found');
        }

        // Generate automatic release notes from GitHub
        let releaseNotesString = '';
        if (previousTag) {
            try {
                const releaseNotesResult = await github.rest.repos.generateReleaseNotes({
                    owner: context.repo.owner,
                    repo: context.repo.repo,
                    tag_name: currentTag,
                    previous_tag_name: previousTag,
                    headers: {
                        'X-GitHub-Api-Version': '2022-11-28',
                        'Accept': 'application/vnd.github+json'
                    }
                })
                releaseNotesString = releaseNotesResult.data["body"]
            } catch (error) {
                releaseNotesString = 'No changes detected between releases.';
            }
        }

        // Parse get_all_manifests.sh to get org/repo information
        const manifestFile = 'get_all_manifests.sh';
        const manifestComponents = parseManifestFile(manifestFile);
        const allComponents = manifestComponents.odh;

        // Create a lookup map for existing components
        const componentLookup = new Map();
        for (const comp of allComponents) {
            componentLookup.set(comp.componentName, comp);
        }

        // Build component release notes from GitOps data
        let outputStr = "## Component Release Notes\n"

        // Add updated components
        if (Object.keys(parsedData).length > 0) {
            for (const [componentName, componentData] of Object.entries(parsedData)) {
                const newRef = componentData.ref;
                const displayName = componentData.display_name;

                // Check if this is a multi-component mapping (e.g., workbenches/notebook-controller)
                const targetComponents = MULTI_COMPONENT_MAPPING[componentName] || [componentName];

                // Use the first target component to get org/repo info (they should all be from same repo)
                const targetComponent = targetComponents[0];
                const existingComponent = componentLookup.get(targetComponent);

                if (existingComponent) {
                    // Use org and repo from manifest, but new ref from GitOps input
                    const githubUrl = `https://github.com/${existingComponent.org}/${existingComponent.repo}/tree/${newRef}`;
                    outputStr += `- **${displayName}**: ${githubUrl}\n`;
                } else {
                    outputStr += `- **${displayName}**: ${newRef}\n`;
                }
            }
        } else {
            outputStr += "No component updates in this release.\n";
        }

        // Add automatic release notes
        outputStr += "\n" + releaseNotesString;

        core.setOutput('release-notes-body', outputStr);
    } catch (error) {
        core.setFailed(`Action failed with error ${error}`);
    }
}
