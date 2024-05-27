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
                components = components.splice(2, components.length - 1)
                components.forEach(component => {
                    [componentName, branchUrl] = component.split("|")
                    const splitArr = branchUrl.split("/")
                    const idx = splitArr.indexOf("tree")
                    const branchName = splitArr.slice(idx + 1).join("/")
                    if(componentName === "notebook-controller"){
                        core.exportVariable("component_spec_odh-notebook-controller".toLowerCase(), branchName);
                        core.exportVariable("component_spec_kf-notebook-controller".toLowerCase(), branchName);
                    }else{
                        core.exportVariable("component_spec_"+componentName.toLowerCase(), branchName);
                    }
                })
            }
        })
        console.log("Read release/tag from tracker issue successfully...")
    }).catch(e => {
        core.setFailed(`Action failed with error ${e}`);
    })
}