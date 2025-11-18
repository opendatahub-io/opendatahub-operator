const { getLatestCommitSha, parseManifestFile, updateManifestFile } = require('./manifest-utils');

module.exports = async function ({ github, core }) {

  const manifestFile = 'get_all_manifests.sh';
  const allComponents = parseManifestFile(manifestFile);

  // Filter to only components with branch@sha format
  const componentsWithSha = new Map();
  for (const [componentName, componentInfo] of allComponents) {
    if (!componentInfo.ref.includes('@')) {
      continue;
    }

    const refParts = componentInfo.ref.split('@');
    if (refParts.length !== 2) {
      console.log(`⚠️  Skipping ${componentName}: invalid ref format "${componentInfo.ref}" (expected "branch@sha")`);
      continue;
    }

    const [branchRef, commitSha] = refParts;
    if (!branchRef || !commitSha) {
      console.log(`⚠️  Skipping ${componentName}: empty branch or SHA in ref "${componentInfo.ref}"`);
      continue;
    }

    componentsWithSha.set(componentName, {
      ...componentInfo,
      branchRef,
      commitSha
    });
  }

  console.log(`Found ${componentsWithSha.size} components with branch@sha format`);

  const updates = new Map();

  for (const [componentName, manifest] of componentsWithSha) {
    console.log(`Checking ${componentName} (${manifest.org}/${manifest.repo}:${manifest.branchRef})...`);

    const latestSha = await getLatestCommitSha(github, manifest.org, manifest.repo, manifest.branchRef);

    if (latestSha && latestSha !== manifest.commitSha) {
      console.log(`Update needed for ${componentName}: ${manifest.commitSha.substring(0, 8)} → ${latestSha.substring(0, 8)}`);

      updates.set(componentName, {
        org: manifest.org,
        repo: manifest.repo,
        newRef: `${manifest.branchRef}@${latestSha}`,
        sourcePath: manifest.sourcePath,
        originalLine: manifest.originalLine,
        logMessage: `✅ Updated ${componentName}: ${manifest.commitSha.substring(0, 8)} → ${latestSha.substring(0, 8)}`
      });
    } else {
      console.log(`No update needed for ${componentName}`);
    }
  }

  // Set outputs
  const hasUpdates = updates.size > 0;
  core.setOutput('updates-needed', hasUpdates);

  if (!hasUpdates) {
    console.log('All manifest references are up to date');
    return;
  }

  // Update manifest file
  console.log('Updating manifest file...');
  updateManifestFile(manifestFile, updates);

  console.log(`Successfully processed ${updates.size} manifest updates`);
}
