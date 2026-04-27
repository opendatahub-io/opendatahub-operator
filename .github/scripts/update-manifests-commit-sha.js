const { getLatestCommitSha, parseManifestFile, updateManifestFile, filterComponentsWithBranchSha } = require('./manifest-utils');

module.exports = async function ({ github, core }) {
  const manifestFile = 'get_all_manifests.sh';
  const parsedManifests = parseManifestFile(manifestFile);

  const updates = [];

  // Process both ODH and RHOAI platforms
  for (const components of [parsedManifests.odh, parsedManifests.rhoai, parsedManifests.odhCharts, parsedManifests.rhoaiCharts]) {
    const componentsWithSha = filterComponentsWithBranchSha(components);

    console.log(`Found ${componentsWithSha.length} ${componentsWithSha.length > 0 ? componentsWithSha[0].platform.toUpperCase() : ''} components with branch@sha format`);

    for (const manifest of componentsWithSha) {
      console.log(`Checking ${manifest.platform}:${manifest.componentName} (${manifest.org}/${manifest.repo}:${manifest.branchRef})...`);

      const latestSha = await getLatestCommitSha(github, manifest.org, manifest.repo, manifest.branchRef);

      if (latestSha && latestSha !== manifest.commitSha) {
        console.log(`Update needed for ${manifest.platform}:${manifest.componentName}: ${manifest.commitSha.substring(0, 8)} → ${latestSha.substring(0, 8)}`);

        updates.push({
          componentName: manifest.componentName,
          org: manifest.org,
          repo: manifest.repo,
          newRef: `${manifest.branchRef}@${latestSha}`,
          sourcePath: manifest.sourcePath,
          originalLine: manifest.originalLine,
          logMessage: `✅ Updated ${manifest.platform}:${manifest.componentName}: ${manifest.commitSha.substring(0, 8)} → ${latestSha.substring(0, 8)}`
        });
      } else {
        console.log(`No update needed for ${manifest.platform}:${manifest.componentName}`);
      }
    }
  }

  // Set outputs
  const hasUpdates = updates.length > 0;
  core.setOutput('updates-needed', hasUpdates);

  if (!hasUpdates) {
    console.log('All manifest references are up to date');
    return;
  }

  // Update manifest file
  console.log('Updating manifest file...');
  updateManifestFile(manifestFile, updates);

  console.log(`Successfully processed ${updates.length} manifest updates`);
}
