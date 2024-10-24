function getModifiedComponentName(name) {
    let modifiedWord = name.split("-").join(" ").replace(/[^a-zA-Z ]/g, "").trim()
    modifiedWord = modifiedWord[0].toUpperCase() + modifiedWord.slice(1).toLowerCase()
    return modifiedWord.replace("Odh", "ODH")
}
module.exports = async ({ github, core, context }) => {
    const { TRACKER_URL: trackerUrl, VERSION: currentTag } = process.env
    console.log(`The TRACKER_URL is ${trackerUrl}`)
    const arr = trackerUrl.split("/")
    const owner = arr[3]
    const repo = arr[4]
    const issue_number = arr[6]

    try {
        const latestReleaseResult = await github.rest.repos.getLatestRelease({
            owner: context.repo.owner,
            repo: context.repo.repo,
            headers: {
                'X-GitHub-Api-Version': '2022-11-28',
                'Accept': 'application/vnd.github+json',
            }
        })
        const previousTag = latestReleaseResult.data["tag_name"]
        console.log(`The current tag is: ${previousTag}`)

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
        const releaseNotesString = releaseNotesResult.data["body"]

        const commentsResult = await github.rest.issues.listComments({
            owner,
            repo,
            issue_number,
            headers: {
                'X-GitHub-Api-Version': '2022-11-28',
                'Accept': 'application/vnd.github.text+json'
            }
        })
        let outputStr = "## Component Release Notes\n"
        commentsResult.data.forEach((issue) => {
            let issueCommentBody = issue.body_text
            if (issueCommentBody.includes("#Release#")) {
                let components = issueCommentBody.split("\n")
                const releaseIdx = components.indexOf("#Release#")
                components = components.splice(releaseIdx + 1, components.length)
                const regex = /\s*[A-Za-z-_0-9]+\s*\|\s*(https:\/\/github\.com\/.*(tree|releases).*){1}\s*\|?\s*(https:\/\/github\.com\/.*releases.*)?\s*/;
                components.forEach(component => {
                    if (regex.test(component)) {
                        let [componentName, branchUrl, tagUrl] = component.split("|")
                        componentName = getModifiedComponentName(componentName.trim())
                        const releaseNotesUrl = (tagUrl || branchUrl).trim();
                        if (!outputStr.includes(componentName)) outputStr += `- **${componentName}**: ${releaseNotesUrl}\n`

                    }
                })
            }
        })
        outputStr += "\n" + releaseNotesString
        console.log("Created component release notes successfully...")
        core.setOutput('release-notes-body', outputStr);
    } catch (error) {
        core.setFailed(`Action failed with error ${error}`);
    }
}