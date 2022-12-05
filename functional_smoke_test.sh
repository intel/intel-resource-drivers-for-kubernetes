#!/bin/bash

set -eu

#########################################################################
# This will:
# - ensure cluster does not have PODs named like test pods
# - deploy one by one scenarios
# - ensure expected outcome reached within timeout
#########################################################################

TEST_POD_NAMES="test-single-pod test-inline-claim"

# names of functions
#TESTS_TO_RUN="successful_single_pod"

# in seconds, how much until pod is expected to be in RUNNING state
POD_RUNNING_TIMEOUT=5
# how many times wait before declaring failure
POD_RUNNING_TIMEOUT_COUNT=2  # times POD_RUNNING_TIMEOUT

function ensure_no_pods_with_test_names {
    namespace=${1:-default}
    pods=$(kubectl get pods --no-headers -o custom-columns=NAME:.metadata.name -n "$namespace")
    for test_pod_name in $TEST_POD_NAMES; do
        if echo "$pods" | grep -q "$test_pod_name"; then
            echo "Existing Pod $test_pod_name will conflict with test pods. Exiting."
            exit 1
        fi
    done
}

function wait_for_pod_deletion {
    if [[ $# -lt 1 ]]; then
        echo "Missing argument in ${FUNCNAME[0]}"
        echo 1
    fi

    podname=$1
    for counter in $(seq 0 $POD_RUNNING_TIMEOUT_COUNT); do
        echo "Waiting $counter time"
        sleep $POD_RUNNING_TIMEOUT
        if ! kubectl get pods --no-headers -o custom-columns=NAME:.metadata.name -n "$namespace" | grep -q "$podname"; then
            return 0
        fi
    done
    echo "Error waiting for Pod deletion. Pod $podname is still present. Exiting."
    exit 1
}

function pod_is_running {
    if [[ $# -lt 1 ]]; then
        echo "Missing argument in ${FUNCNAME[0]}"
        exit 1
    fi
    podname=$1
    namespace=${2:-default}
    pod_state=$(kubectl get pods --no-headers -o custom-columns=NAME:.metadata.name,STATUS:.status.phase -n "$namespace" | grep "$podname" | awk '{print $2}')

    echo "Pod $podname state: $pod_state"

    if [[ "$pod_state" != "Running" ]]; then
        return 1
    fi

    return 0
}

function successful_single_pod {
    yamls_dir="deployments/tests/"
    deployments=(
"delayed-pod-external-deployment.yaml"
"delayed-pod-external.yaml"
"delayed-pod-inline-deployment.yaml"
"delayed-pod-inline.yaml"
"immediate-pod-external-deployment.yaml"
"immediate-pod-inline-deployment.yaml"
"immediate-pod-inline.yaml"
)

    for yaml in ${deployments[@]}; do
        echo ""
        echo "-------------- $yaml --------------"
        yaml_file="$yamls_dir/$yaml"
        kubectl apply -f "$yaml_file"
        OK=false
        for counter in $(seq 0 $POD_RUNNING_TIMEOUT_COUNT); do
            echo "Waiting for Pod to get up. $counter time"
            sleep $POD_RUNNING_TIMEOUT
            if pod_is_running "test-single-pod.*"; then
                echo "Deleting $yaml_file"
                kubectl delete -f "$yaml_file"
                wait_for_pod_deletion "test-single-pod.*"
                OK=true
                break
            fi
        done
        if [[ ! $OK ]]; then
            echo "Pod did not get to RUNNING state in time"
            exit 1
        fi
    done

    echo "-------- All OK --------"
}


ensure_no_pods_with_test_names
successful_single_pod
