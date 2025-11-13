const fs = require('fs');

/**
 * Shared utilities for manifest file operations
 * Used by both update-manifests-tags.js and update-manifests-commit-sha.js
 */

/**
 * Get the latest commit SHA for a repository reference
 * @param {object} github - GitHub API client (octokit)
 * @param {string} org - Repository organization
 * @param {string} repo - Repository name
 * @param {string} ref - Branch, tag, or commit reference
 * @returns {Promise<string|null>} The commit SHA or null if failed
 */
async function getLatestCommitSha(github, org, repo, ref) {
    try {
        console.log(`Fetching latest commit SHA for ${org}/${repo}:${ref}`);
        const { data } = await github.rest.repos.getCommit({
            owner: org,
            repo: repo,
            ref: ref
        });
        return data.sha;
    } catch (error) {
        console.error(`Failed to fetch commit SHA for ${org}/${repo}:${ref}: ${error.message}`);
        return null;
    }
}

/**
 * Parse the get_all_manifests.sh file to extract component definitions
 * @param {string} filePath - Path to the manifest file
 * @returns {Map} Map of component name to component info
 */
function parseManifestFile(filePath) {
    const content = fs.readFileSync(filePath, 'utf8');
    const components = new Map();

    // Regex to match component manifest definitions (line by line)
    // Pattern: ["component"]="org:repo:ref:path"
    const manifestRegex = /^\s*\["([^"]+)"\]="([^:]+):([^:]+):([^:]+):([^"]+)"$/;

    const lines = content.split('\n');
    for (const line of lines) {
        const match = line.match(manifestRegex);
        if (!match) {
            continue;
        }

        const [fullMatch, componentName, org, repo, ref, sourcePath] = match;

        components.set(componentName, {
            org,
            repo,
            ref,
            sourcePath,
            originalLine: fullMatch.trim()
        });
    }

    return components;
}

/**
 * Update the manifest file with new component information
 * @param {string} filePath - Path to the manifest file
 * @param {Map} updates - Map of component name to update info
 */
function updateManifestFile(filePath, updates) {
    if (updates.size === 0) {
        console.log('No updates to apply');
        return false;
    }

    let content = fs.readFileSync(filePath, 'utf8');
    let hasChanges = false;

    for (const [componentName, updateInfo] of updates) {
        const oldLine = updateInfo.originalLine;
        const newLine = `["${componentName}"]="${updateInfo.org}:${updateInfo.repo}:${updateInfo.newRef}:${updateInfo.sourcePath}"`;

        if (content.includes(oldLine)) {
            content = content.replace(oldLine, newLine);
            hasChanges = true;
            if (updateInfo.logMessage) {
                console.log(updateInfo.logMessage);
            }
        } else {
            console.log(`Warning: Could not find ${componentName} in manifest file`);
        }
    }

    if (hasChanges) {
        fs.writeFileSync(filePath, content);
    }

    return hasChanges;
}

module.exports = {
    getLatestCommitSha,
    parseManifestFile,
    updateManifestFile
};
