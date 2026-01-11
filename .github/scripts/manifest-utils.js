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
 * Parse a manifest block from the content
 * @param {string} content - Full file content
 * @param {string} arrayName - Name of the array to extract (e.g., 'ODH_COMPONENT_MANIFESTS')
 * @param {string} platform - Platform identifier ('odh' or 'rhoai')
 * @returns {Array} Array of component info objects
 */
function parseManifestBlock(content, arrayName, platform) {
    const components = [];

    // Extract the entire manifest block using regex
    const blockRegex = new RegExp(
        `declare -A ${arrayName}=\\(([\\s\\S]*?)\\n\\)`,
        'm'
    );
    const blockMatch = content.match(blockRegex);

    if (!blockMatch) {
        return components;
    }

    const blockContent = blockMatch[1];

    // Regex to match component manifest definitions (line by line)
    // Pattern: ["component"]="org:repo:ref:path"
    const manifestRegex = /\["([^"]+)"\]="([^:]+):([^:]+):([^:]+):([^"]+)"/g;

    let match;
    while ((match = manifestRegex.exec(blockContent)) !== null) {
        const [fullMatch, componentName, org, repo, ref, sourcePath] = match;

        components.push({
            componentName,
            org,
            repo,
            ref,
            sourcePath,
            originalLine: fullMatch.trim(),
            platform
        });
    }

    return components;
}

/**
 * Parse the get_all_manifests.sh file to extract component definitions
 * Now supports both ODH and RHOAI platform types
 * @param {string} filePath - Path to the manifest file
 * @returns {object} Object containing:
 *   - odh: Array of component info for ODH
 *   - rhoai: Array of component info for RHOAI
 */
function parseManifestFile(filePath) {
    const content = fs.readFileSync(filePath, 'utf8');

    // Parse both ODH and RHOAI manifest blocks
    const odhComponents = parseManifestBlock(content, 'ODH_COMPONENT_MANIFESTS', 'odh');
    const rhoaiComponents = parseManifestBlock(content, 'RHOAI_COMPONENT_MANIFESTS', 'rhoai');

    return {
        odh: odhComponents,
        rhoai: rhoaiComponents
    };
}

/**
 * Update the manifest file with new component information
 * @param {string} filePath - Path to the manifest file
 * @param {Array} updates - Array of update objects containing componentName and update info
 */
function updateManifestFile(filePath, updates) {
    if (!updates || updates.length === 0) {
        console.log('No updates to apply');
        return false;
    }

    let content = fs.readFileSync(filePath, 'utf8');
    let hasChanges = false;

    for (const update of updates) {
        const { componentName, org, repo, newRef, sourcePath, originalLine, logMessage } = update;
        const oldLine = originalLine;
        const newLine = `["${componentName}"]="${org}:${repo}:${newRef}:${sourcePath}"`;

        if (content.includes(oldLine)) {
            content = content.replace(oldLine, newLine);
            hasChanges = true;
            if (logMessage) {
                console.log(logMessage);
            }
        } else {
            console.log(`Warning: Could not find component in manifest file (originalLine: ${oldLine})`);
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
