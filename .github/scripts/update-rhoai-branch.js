const { getLatestCommitSha, parseManifestFile, updateManifestFile, filterComponentsWithBranchSha } = require('./manifest-utils');

module.exports = async function ({ github, core }) {
  const newBranch = process.env.NEW_RHOAI_BRANCH;
  if (!newBranch) {
    core.setFailed('NEW_RHOAI_BRANCH environment variable is required');
    return;
  }

  console.log(`Updating RHOAI manifests to branch: ${newBranch}`);

  const manifestFile = 'get_all_manifests.sh';
  const parsedManifests = parseManifestFile(manifestFile);

  const updates = [];
  const missingBranches = [];

  for (const components of [parsedManifests.rhoai, parsedManifests.rhoaiCharts]) {
    const componentsWithSha = filterComponentsWithBranchSha(components);

    console.log(`Found ${componentsWithSha.length} RHOAI components with branch@sha format`);

    for (const manifest of componentsWithSha) {
      console.log(`Updating ${manifest.componentName} (${manifest.org}/${manifest.repo}) to branch ${newBranch}...`);

      const latestSha = await getLatestCommitSha(github, manifest.org, manifest.repo, newBranch);

      if (!latestSha) {
        missingBranches.push(`${manifest.org}/${manifest.repo}`);
        continue;
      }

      updates.push({
        componentName: manifest.componentName,
        org: manifest.org,
        repo: manifest.repo,
        newRef: `${newBranch}@${latestSha}`,
        sourcePath: manifest.sourcePath,
        originalLine: manifest.originalLine,
        logMessage: `✅ Updated ${manifest.componentName}: ${manifest.ref} → ${newBranch}@${latestSha.substring(0, 8)}`
      });
    }
  }

  if (missingBranches.length > 0) {
    core.setFailed(`Branch "${newBranch}" was not found in: ${missingBranches.join(', ')}`);
    return;
  }

  const hasUpdates = updates.length > 0;
  core.setOutput('updates-needed', hasUpdates);

  if (!hasUpdates) {
    console.log('No RHOAI manifest updates needed');
    return;
  }

  console.log('Updating manifest file...');
  updateManifestFile(manifestFile, updates);

  console.log(`Successfully updated ${updates.length} RHOAI manifest references to branch ${newBranch}`);
}
