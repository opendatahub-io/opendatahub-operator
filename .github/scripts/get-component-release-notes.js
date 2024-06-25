module.exports = ({ github, core }) => {
    const { TRACKER_URL } = process.env
    console.log(`The TRACKER_URL is ${TRACKER_URL}`)
    const arr = TRACKER_URL.split("/")
    const owner = arr[3]
    const repo = arr[4]
    const issue_number = arr[6]

    github.request('GET /repos/{owner}/{repo}/issues/{issue_number}/comments', {
        owner,
        repo,
        issue_number,
        headers: {
            'X-GitHub-Api-Version': '2022-11-28',
            'Accept': 'application/vnd.github.text+json'
        }
    }).then((result) => {
        let outputStr = "## Component Release Notes\n"
        result.data.forEach((issue) => {
            issueCommentBody = issue.body_text
            if (issueCommentBody.includes("#Release#")) {
                let components = issueCommentBody.split("\n")
                const releaseIdx = components.indexOf("#Release#")
                components = components.splice(releaseIdx + 1, components.length)
                const regex = /[A-Za-z-_0-9]+\|(https:\/\/github\.com\/.*tree.*){1}\|(https:\/\/github\.com\/.*releases.*){1}/;
                components.forEach(component => {
                    if (regex.test(component.trim())) {
                        const [componentName, branchUrl, tagUrl] = component.split("|")
                        outputStr += `- **${componentName.trim().charAt(0).toUpperCase() + componentName.trim().slice(1)}**: ${tagUrl.trim()}\n`
                    }
                })
            }
        })
        console.log("Created component release notes successfully...")
        core.setOutput('release-notes-body', outputStr);
    }).catch(e => {
        core.setFailed(`Action failed with error ${e}`);
    })
}