module.exports = ({ github, core }) => {
    const { TRACKER_URL } = process.env
    console.log(`The tracker url is: ${TRACKER_URL}`)

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
        result.data.forEach((issue) => {
            issueCommentBody = issue.body_text
            if (issueCommentBody.includes("#Release#")) {
                let components = issueCommentBody.split("\n")
                const releaseIdx = components.indexOf("#Release#")
                components = components.splice(releaseIdx + 1, components.length)
                const regex = /[A-Za-z-_0-9]+\|(https:\/\/github\.com\/.*tree.*){1}\|(https:\/\/github\.com\/.*releases.*){1}/;
                components.forEach(component => {
                    if (regex.test(component)) {
                        const [componentName, branchUrl] = component.split("|")
                        const splitArr = branchUrl.trim().split("/")
                        const idx = splitArr.indexOf("tree")
                        const branchName = splitArr.slice(idx + 1).join("/")
                        if (componentName.trim() === "notebook-controller") {
                            core.exportVariable("component_spec_odh-notebook-controller".toLowerCase(), branchName);
                            core.exportVariable("component_spec_kf-notebook-controller".toLowerCase(), branchName);
                        } else {
                            core.exportVariable("component_spec_" + componentName.toLowerCase(), branchName);
                        }
                    }
                })
            }
        })
        console.log("Read release/tag from tracker issue successfully...")
    }).catch(e => {
        core.setFailed(`Action failed with error ${e}`);
    })
}