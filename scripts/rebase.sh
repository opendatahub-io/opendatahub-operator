#!/usr/bin/bash

UPSTREAM=${UPSTREAM:-"https://github.com/kubeflow/kfctl"}
TARGET_REPO=${TARGET_REPO:-"https://github.com/opendatahub-io/opendatahub-operator"}
REBASE_TO=${REBASE_TO:-"FETCH_HEAD"}
OPERATOR_IMG_NAME=${OPERATOR_IMG:-quay.io/${USER}/opendatahub-operator}
GITHUB_TOKEN=${GITHUB_TOKEN}
PUSH_OPERATOR=${PUSH_OPERATOR:-true}

REF=$(git rev-parse --short ${REBASE_TO})
BRANCH_NAME="kfctl/master-${REF}"
OPERATOR_IMG=${OPERATOR_IMG_NAME}:rebase-${REF}


[ -z ${GITHUB_TOKEN} ] && { echo "ERROR: You need to provide your Github Token in GITHUB_TOKEN environment variable."; exit 1; }

function parse_repo_org() {
    local repo=$1
    echo $(echo ${repo} | sed 's#.*github.com\(/\|:\)\([^/]*\)/.*#\2#')
}

function parse_repo_name() {
    local repo=$1
    echo $(echo ${repo} | sed 's#.*/\([^.]*\).*#\1#')
}

function push_operator_image() {
    REPO_DIR=$([[ ${PWD} == *scripts ]] && dirname ${PWD})
    [[ ${REPO_DIR} != ${PWD} ]] && pushd ${REPO_DIR}
    make build-and-push-operator OPERATOR_IMG=${OPERATOR_IMG}
    [[ ${REPO_DIR} != ${PWD} ]] && popd
}

TARGET_REPO_ORG=$(parse_repo_org ${TARGET_REPO})
TARGET_REPO_NAME=$(parse_repo_name ${TARGET_REPO})

ORIGIN_REPO=$(git remote get-url origin --push)
ORIGIN_REPO_ORG=$(parse_repo_org ${ORIGIN_REPO})
ORIGIN_REPO_NAME=$(parse_repo_name ${ORIGIN_REPO})

git fetch ${TARGET_REPO}
git checkout -b ${BRANCH_NAME} FETCH_HEAD
git fetch ${UPSTREAM}
git rebase ${REBASE_TO}
git push --set-upstream origin ${BRANCH_NAME}

if [[ ${PUSH_OPERATOR} == "true" ]]; then
    push_operator_image
    OPERATOR_IMAGE_MSG="\n\nOperator image built from this change is available at \`${OPERATOR_IMG}\`"
    echo -e ${OPERATOR_IMAGE_MSG}
fi

PR_BODY_FILE=/tmp/pr_rebase_${REF}

cat > ${PR_BODY_FILE} <<EOF
{
  "title": "Rebase to kfctl master (${REF})",
  "body": "Rebase to kfctl master at commit [${REF}](https://github.com/kubeflow/kfctl/commit/${REF})${OPERATOR_IMAGE_MSG}\n\n Do not forget to use **Rebase and Merge** option for merging",
  "head": "${ORIGIN_REPO_ORG}:${BRANCH_NAME}",
  "base": "master"
}
EOF

API_URL=https://api.github.com/repos/${TARGET_REPO_ORG}/${TARGET_REPO_NAME}/pulls

echo "Creating PR: ${API_URL}"

RESPONSE=$(curl -X POST -H "Authorization: token ${GITHUB_TOKEN}" -H "Content-Type: application/json" ${API_URL} -d @${PR_BODY_FILE})
PR_URL=$(echo ${RESPONSE} | jq -r '.html_url')

[ ${PR_URL} == "null" ] && echo ${RESPONSE}

echo -e "\n\nRebase PR created at ${PR_URL}"
