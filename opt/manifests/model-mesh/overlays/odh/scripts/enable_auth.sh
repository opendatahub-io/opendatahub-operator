#!/bin/bash

set -e -o pipefail
export ETCDCTL_API=3

function etcd::availability() {
    local cmd=$1 # Command whose output we require
    local interval=$2 # How many seconds to sleep between tries
    local iterations=$3 # How many times we attempt to run the command

    ii=0

    while [ $ii -le $iterations ]
    do

        token=$($cmd) && returncode=$? || returncode=$?
        if [ $returncode -eq 0 ]; then
            break
        fi

        ((ii=ii+1))
        if [ $ii -eq 100 ]; then
            echo $cmd "did not return a value"
            exit 1
        fi
        sleep $interval
    done
    echo $token
}

cmd='etcdctl --endpoints=http://0.0.0.0:2379 endpoint health'

etcd::availability "${cmd}" 6 10

PASSWORD="${1:-password}"

echo $PASSWORD | etcdctl --endpoints=http://0.0.0.0:2379 user add root --interactive=false
etcdctl --endpoints=http://0.0.0.0:2379 auth enable
