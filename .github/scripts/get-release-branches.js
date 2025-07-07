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
            let issueCommentBody = issue.body_text
            if (issueCommentBody.includes("#Release#")) {
                let components = issueCommentBody.split("\n")
                const releaseIdx = components.indexOf("#Release#")
                components = components.splice(releaseIdx + 1, components.length)
                const regex = /\s*[A-Za-z-_0-9]+\s*\|\s*(https:\/\/github\.com\/.*(tree|releases).*){1}\s*\|?\s*(https:\/\/github\.com\/.*releases.*)?\s*/;
                components.forEach(component => {
                    if (regex.test(component)) {
                        const [componentName, branchOrTagUrl] = component.split("|")
                        const splitArr = branchOrTagUrl.trim().split("/")
                        let idx = null
                        if (splitArr.includes("tag")) {
                            idx = splitArr.indexOf("tag")
                        } else if (splitArr.includes("tree")) {
                            idx = splitArr.indexOf("tree")
                        }
                        const branchName = splitArr.slice(idx + 1).join("/")
                        const repoOrg = splitArr[3]

                        if (componentName.trim() === "workbenches/notebook-controller") {
                            core.exportVariable("component_spec_odh-notebook-controller".toLowerCase(), branchName);
                            core.exportVariable("component_spec_kf-notebook-controller".toLowerCase(), branchName);
                            core.exportVariable("component_org_odh-notebook-controller".toLowerCase(), repoOrg);
                            core.exportVariable("component_org_kf-notebook-controller".toLowerCase(), repoOrg);
                        } else {
                            core.exportVariable("component_spec_" + componentName.trim().toLowerCase(), branchName);
                            core.exportVariable("component_org_" + componentName.trim().toLowerCase(), repoOrg);
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
