
function getComponentName(name){
    switch(name){
        case "codeflare":
            return "Codeflare"
        case "ray":
            return "Ray"
        case "kueue":
            return "Kueue"
        case "data-science-pipelines-operator":
            return "Datascience Pipelines"
        case "odh-dashboard":
            return "Dashboard"
        case "notebook-controller":
            return "Notebook controller"
        case "notebooks":
            return "Notebooks"
        case "trustyai":
            return "TrustyAI"
        case "model-mesh":
            return "Model mesh"
        case "odh-model-controller":
            return "ODH Model Controller"
        case "kserve":
            return "Kserve"
        case "modelregistry":
            return "Model Registry"
        case "trainingoperator":
            return "Training operator"
        default:
            return null
    }   
}

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
        let servingComponents = '- **Serving**\n\t'
        let distributedWorkloadComponents = '- **Distributed Workloads**\n\t'
        let workbenchComponents= '- **Workbench**\n\t'
        result.data.forEach((issue) => {
            let issueCommentBody = issue.body_text
            if (issueCommentBody.includes("#Release#")) {
                let components = issueCommentBody.split("\n")
                const releaseIdx = components.indexOf("#Release#")
                components = components.splice(releaseIdx + 1, components.length)
                const regex = /\s*[A-Za-z-_0-9]+\s*\|\s*(https:\/\/github\.com\/.*(tree|releases).*){1}\s*\|?\s*(https:\/\/github\.com\/.*releases.*)?\s*/;
                components.forEach(component => {
                    if (regex.test(component)) {
                        let [componentName, branchUrl, tagUrl] = component.split("|")
                        componentName = componentName.trim()
                        let releaseNotesUrl = tagUrl
                        if(!releaseNotesUrl){
                            releaseNotesUrl = branchUrl
                        }
                        releaseNotesUrl = releaseNotesUrl.trim()
                        if(["model-mesh", "kserve","odh-model-controller"].includes(componentName)){
                           servingComponents+=` - ${getComponentName(componentName)}: ${releaseNotesUrl}\n\t`
                        }else if(["codeflare", "ray", "kueue", "trainingoperator"].includes(componentName)){
                            distributedWorkloadComponents+=` - ${getComponentName(componentName)}: ${releaseNotesUrl}\n\t`
                        }else if(["notebooks", "notebook-controller"].includes(componentName)){
                            workbenchComponents+=` - ${getComponentName(componentName)}: ${releaseNotesUrl}\n\t`
                        }else{
                            outputStr+= getComponentName(componentName)?`- **${getComponentName(componentName)}**: ${releaseNotesUrl}\n`:""
                        }
        
                    }
                })
            }
        })
        outputStr+=servingComponents.slice(0,-1)+distributedWorkloadComponents.slice(0,-1)+workbenchComponents.slice(0,-1)
        console.log("Created component release notes successfully...")
        core.setOutput('release-notes-body', outputStr);
    }).catch(e => {
        core.setFailed(`Action failed with error ${e}`);
    })
}