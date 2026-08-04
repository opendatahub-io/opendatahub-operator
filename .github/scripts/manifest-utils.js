const fs = require('fs');
const { execFileSync } = require('child_process');

/**
 * Shared utilities for manifest file operations (YAML-based, using yq for conversion)
 * Used by update-manifests-tags.js, update-manifests-commit-sha.js, and update-rhoai-branch.js
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
 * Read YAML file as JSON object using yq
 * @param {string} filePath - Path to YAML file
 * @returns {object} Parsed data
 */
function readYaml(filePath) {
    const yq = process.env.YQ || 'yq';
    const json = execFileSync(yq, ['eval', '-o=json', '.', filePath], { encoding: 'utf8' });
    return JSON.parse(json);
}

/**
 * Write JSON object back to YAML file using yq.
 * Note: this JSON round-trip does not preserve YAML comments. This is acceptable
 * because the JS scripts only update ref fields in the components/charts sections,
 * not the imageOverrides section which has comments. For comment-preserving edits,
 * use the Go manifest-tools CLI instead.
 * @param {string} filePath - Path to YAML file
 * @param {object} data - Data to write
 */
function writeYaml(filePath, data) {
    const yq = process.env.YQ || 'yq';
    const jsonFile = filePath + '.tmp.json';
    try {
        fs.writeFileSync(jsonFile, JSON.stringify(data, null, 2));
        const yamlOutput = execFileSync(yq, ['eval', '-P', '.', jsonFile], { encoding: 'utf8' });
        fs.writeFileSync(filePath, yamlOutput);
    } finally {
        try { fs.unlinkSync(jsonFile); } catch (_) { /* ignore cleanup errors */ }
    }
}

/**
 * Parse a single section of the YAML config and return component arrays per platform
 * @param {object} sectionData - The section object (e.g., data.components)
 * @param {string} section - Section name ('components', 'ccmCharts', 'componentCharts')
 * @returns {object} { odh: [...], rhoai: [...] }
 */
function parseSectionComponents(sectionData, section) {
    const odh = [];
    const rhoai = [];

    if (!sectionData) {
        return { odh, rhoai };
    }

    for (const [componentName, entry] of Object.entries(sectionData)) {
        if (!entry || typeof entry !== 'object') {
            continue;
        }
        for (const platform of ['odh', 'rhoai']) {
            const platformEntry = entry[platform];
            if (!platformEntry || !platformEntry.repo || !platformEntry.ref) {
                continue;
            }

            const [org, repo] = platformEntry.repo.split('/', 2);

            const component = {
                componentName,
                org,
                repo,
                ref: platformEntry.ref,
                sourcePath: platformEntry.sourcePath,
                originalRef: platformEntry.ref,
                platform,
                section
            };

            if (platform === 'odh') {
                odh.push(component);
            } else {
                rhoai.push(component);
            }
        }
    }

    return { odh, rhoai };
}

/**
 * Parse manifests-config.yaml to extract component definitions
 * @param {string} filePath - Path to manifests-config.yaml
 * @returns {object} Object with odh, rhoai, odhCcmCharts, rhoaiCcmCharts, odhCharts, rhoaiCharts arrays
 */
function parseManifestFile(filePath) {
    const data = readYaml(filePath);

    const components = parseSectionComponents(data.components, 'components');
    const ccmCharts = parseSectionComponents(data.ccmCharts, 'ccmCharts');
    const componentCharts = parseSectionComponents(data.componentCharts, 'componentCharts');

    return {
        odh: components.odh,
        rhoai: components.rhoai,
        odhCcmCharts: ccmCharts.odh,
        rhoaiCcmCharts: ccmCharts.rhoai,
        odhCharts: componentCharts.odh,
        rhoaiCharts: componentCharts.rhoai
    };
}

/**
 * Update the manifest YAML file with new component references
 * @param {string} filePath - Path to manifests-config.yaml
 * @param {Array} updates - Array of update objects with componentName, platform, section, newRef, logMessage
 * @returns {boolean} Whether any changes were made
 */
function updateManifestFile(filePath, updates) {
    if (!updates || updates.length === 0) {
        console.log('No updates to apply');
        return false;
    }

    const data = readYaml(filePath);
    let hasChanges = false;

    for (const update of updates) {
        const { componentName, platform, section, newRef, logMessage } = update;

        const sectionData = data[section];
        if (!sectionData || !sectionData[componentName] || !sectionData[componentName][platform]) {
            console.log(`Warning: Could not find ${section}.${componentName}.${platform} in manifest file`);
            continue;
        }

        const currentRef = sectionData[componentName][platform].ref;
        if (currentRef !== newRef) {
            sectionData[componentName][platform].ref = newRef;
            hasChanges = true;
            if (logMessage) {
                console.log(logMessage);
            }
        }
    }

    if (hasChanges) {
        writeYaml(filePath, data);
    }

    return hasChanges;
}

/**
 * Filter components that use the branch@sha ref format and extract the parts
 * @param {Array} components - Array of component info objects from parseManifestFile
 * @returns {Array} Filtered array with added branchRef and commitSha properties
 */
function filterComponentsWithBranchSha(components) {
    const result = [];
    for (const componentInfo of components) {
        if (!componentInfo.ref.includes('@')) {
            continue;
        }

        const refParts = componentInfo.ref.split('@');
        if (refParts.length !== 2) {
            console.log(`Warning: Skipping ${componentInfo.platform}:${componentInfo.componentName}: invalid ref format "${componentInfo.ref}" (expected "branch@sha")`);
            continue;
        }

        const [branchRef, commitSha] = refParts;
        if (!branchRef || !commitSha) {
            console.log(`Warning: Skipping ${componentInfo.platform}:${componentInfo.componentName}: empty branch or SHA in ref "${componentInfo.ref}"`);
            continue;
        }

        result.push({
            ...componentInfo,
            branchRef,
            commitSha
        });
    }
    return result;
}

module.exports = {
    getLatestCommitSha,
    parseManifestFile,
    updateManifestFile,
    filterComponentsWithBranchSha
};
