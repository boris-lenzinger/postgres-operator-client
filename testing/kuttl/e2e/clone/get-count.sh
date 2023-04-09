#!/bin/bash

function get_count() {
  local p_cluster_name=$1
  local p_namespace=$2
  local p_database=${3:-${p_cluster_name}}

  local master=$(kubectl get pod -n ${p_namespace} \
          -l postgres-operator.crunchydata.com/cluster=${p_cluster_name},postgres-operator.crunchydata.com/role=master \
          -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

  if [ -z "${master}" ]; then
      echo "failed to identify master pod. Stopping process."
      exit 1
  fi

  # Count data
  count=$(kubectl exec -it ${master} -n ${p_namespace} -c database -- psql -d ${p_database} -tz -c "select count(*) from users;"| sed -e "s#\s*##g")
  echo ${count}
}

get_count $*
