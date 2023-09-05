# Contributing to the Opendatahub Operator

Thanks for your interest in this project. You can contribute to this project in many different ways.

## Issues and Enhancements

Please let us know via our GitHub issue tracker if you find a problem, even if you don't have a fix for it.
The ideal issue report should be descriptive, and where possible include the steps we can take to reproduce the problem for ourselves.

Please go to [issue tracker](https://github.com/opendatahub-io/opendatahub-operator/issues) and create a "New issue".
Choose suitable issue type and fill in description as detailed as possible.
Lastly, add extra label for this issue if that can help us refine it.

If you have a proposed fix for an issue, or an enhancement you would like to make to the code please describe it in an issue, then send us the code as a GitHub pull request as described below.

## Pull request

Use a descriptive title, and if it relates to an issue in our tracker please reference which one.
If the PR is not intended to be merged you should prefix the title with "[WIP]" which indicates it is still Work In Progress.
PR's description should provide enough information for a reviewer to understand the changes and their relation to the rest of the code.

## Setting up a Fedora-based development environment

This is a loose list of tools to install on your linux box in order to compile, test and deploy the operator. 

```bash
ssh-keygen -t ed25519 -C "<email-registered-on-github-account>"
# upload public key to github

sudo dnf makecache --refresh
sudo dnf install -y git-all
sudo dnf install -y golang
sudo dnf install -y podman
sudo dnf install -y cri-o kubernetes-kubeadm kubernetes-node kubernetes-client cri-tools
sudo dnf install -y operator-sdk
sudo dnf install -y wget
wget https://mirror.openshift.com/pub/openshift-v4/clients/oc/latest/linux/oc.tar.gz
cd bin/; tar -xzvf ../oc.tar.gz ; cd .. ; rm oc.tar.gz
sudo dnf install -y zsh

# update PATH
echo 'export PATH=${PATH}:~/bin' >> ~/.zshrc
echo 'export GOPROXY=https://proxy.golang.org' >> ~/.zshrc
```

