const { getLatestCommitSha } = require('./manifest-utils');

/**
 * Convert image name to RELATED_IMAGE_* env var name using convention:
 * - Uppercase
 * - Replace hyphens with underscores
 * - Prefix: RELATED_IMAGE_ODH_
 * - Suffix: _IMAGE (unless already ends with _IMAGE)
 *
 * Examples:
 *   kube-auth-proxy → RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
 *   foo-image       → RELATED_IMAGE_ODH_FOO_IMAGE (no duplication)
 */
function imageNameToEnvVar(imageName) {
    const normalized = imageName
        .toUpperCase()
        .replace(/[-]/g, '_');

    // Avoid duplication if image name already ends with _IMAGE
    if (normalized.endsWith('_IMAGE')) {
        return `RELATED_IMAGE_ODH_${normalized}`;
    }

    return `RELATED_IMAGE_ODH_${normalized}_IMAGE`;
}

module.exports = async ({ github, core }) => {
    const { TRACKER_URL } = process.env
    console.log(`The tracker url is: ${TRACKER_URL}`)

    const arr = TRACKER_URL.split("/")
    const owner = arr[3]
    const repo = arr[4]
    const issue_number = arr[6]

    try {
        const result = await github.request('GET /repos/{owner}/{repo}/issues/{issue_number}/comments', {
            owner,
            repo,
            issue_number,
            headers: {
                'X-GitHub-Api-Version': '2022-11-28',
                'Accept': 'application/vnd.github.text+json'
            }
        });

        const regex = /\s*[A-Za-z-_0-9/]+\s*\|\s*(https:\/\/github\.com\/.*(tree|releases).*){1}\s*\|?\s*(https:\/\/github\.com\/.*releases.*)?\s*/;

        for (const issue of result.data) {
            const issueCommentBody = issue.body_text;
            if (!issueCommentBody.includes("#Release#")) {
                continue;
            }

            const lines = issueCommentBody.split("\n");
            const releaseIdx = lines.indexOf("#Release#");
            const componentLines = lines.slice(releaseIdx + 1);

            for (const component of componentLines) {
                if (!regex.test(component)) {
                    continue;
                }

                const [componentName, branchOrTagUrl] = component.split("|");
                const splitArr = branchOrTagUrl.trim().split("/");

                let idx = null;
                if (splitArr.includes("tag")) {
                    idx = splitArr.indexOf("tag");
                } else if (splitArr.includes("tree")) {
                    idx = splitArr.indexOf("tree");
                }

                const branchName = splitArr.slice(idx + 1).join("/");
                const repoOrg = splitArr[3];
                const repoName = splitArr[4];
                const trimmedComponentName = componentName.trim();
                console.log(`Processing component: ${trimmedComponentName}`);

                const commitSha = await getLatestCommitSha(github, repoOrg, repoName, branchName);

                // Handle special case for notebook-controller
                if (trimmedComponentName === "workbenches/notebook-controller") {
                    core.exportVariable("component_spec_odh-notebook-controller".toLowerCase(), branchName);
                    core.exportVariable("component_spec_kf-notebook-controller".toLowerCase(), branchName);
                    core.exportVariable("component_org_odh-notebook-controller".toLowerCase(), repoOrg);
                    core.exportVariable("component_org_kf-notebook-controller".toLowerCase(), repoOrg);

                    if (commitSha) {
                        core.exportVariable("component_sha_odh-notebook-controller".toLowerCase(), commitSha);
                        core.exportVariable("component_sha_kf-notebook-controller".toLowerCase(), commitSha);
                    }
                } else {
                    const normalizedName = trimmedComponentName.toLowerCase().replace(/\//g, '-');
                    core.exportVariable("component_spec_" + normalizedName, branchName);
                    core.exportVariable("component_org_" + normalizedName, repoOrg);

                    if (commitSha) {
                        core.exportVariable("component_sha_" + normalizedName, commitSha);
                        console.log(`Set SHA for ${trimmedComponentName}: ${commitSha.substring(0, 8)}`);
                    }
                }
            }

            // Parse #Images# section for operator-level images
            if (issueCommentBody.includes("#Images#")) {
                console.log("Found #Images# section in tracker comment");

                const imagesIdx = lines.indexOf("#Images#");
                const imageLines = lines.slice(imagesIdx + 1);
                const imageRegex = /\s*([A-Za-z0-9\-_]+)\s*\|\s*([a-z0-9.\-]+(?::[0-9]+)?\/[a-zA-Z0-9_.\-\/]+:[a-zA-Z0-9_.\-]+)\s*/;

                for (const imageLine of imageLines) {
                    if (!imageRegex.test(imageLine)) {
                        // Stop processing when we hit a line that doesn't match
                        // (likely end of #Images# section or empty line)
                        if (imageLine.trim() !== "") {
                            break;
                        }
                        continue;
                    }

                    const match = imageLine.match(imageRegex);
                    const imageName = match[1].trim();
                    const imageReference = match[2].trim();

                    console.log(`Processing operator image: ${imageName} -> ${imageReference}`);

                    // Convert image name to env var name using convention
                    const envVarName = imageNameToEnvVar(imageName);

                    // Export the env var
                    core.exportVariable(envVarName, imageReference);
                    console.log(`  ✓ Exported ${envVarName}=${imageReference}`);
                }
            }
        }

        console.log("Read release/tag from tracker issue successfully...");
    } catch (e) {
        core.setFailed(`Action failed with error ${e}`);
    }
}
