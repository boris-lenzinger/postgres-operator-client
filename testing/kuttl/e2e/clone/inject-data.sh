#!/bin/bash

function inject_data() {
  local p_cluster_name=$1
  local p_namespace=$2

  echo "Searching for the pod to contact to insert data..."
  # shellcheck disable=SC2155
  local pod_name=$(kubectl get pod -n "${p_namespace}" \
          -l postgres-operator.crunchydata.com/cluster=${p_cluster_name},postgres-operator.crunchydata.com/role=master \
          -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

  echo kubectl get pod -n "${p_namespace}" -l postgres-operator.crunchydata.com/cluster=${p_cluster_name},postgres-operator.crunchydata.com/role=master -o jsonpath='{.items[0].metadata.name}'

  if [ -z "${pod_name}" ]; then
    echo "failed to identify pod. Stopping process."
    exit 1
  fi

  # Injecter les données
  local database=${p_cluster_name}
  echo "Database identified as : ${database}"

  echo "Generating insert file"
  # shellcheck disable=SC2155
  local tmp_file=$(mktemp INSERT_XXXXXX.sql)
  cat >"${tmp_file}"<<EOF
\c ${database};
CREATE TABLE IF NOT EXISTS users (id serial, name varchar);
EOF
  trap "rm ${tmp_file}" SIGINT SIGTERM EXIT

  for  i in $(seq 1 100); do
    printf "insert into users(name) values ('name-%d');" "$i" >> "${tmp_file}"
  done
  echo "Done"

  echo "Inserting rows."
  cat "${tmp_file}" | kubectl exec -it "${pod_name}" -n "${p_namespace}" -c database -- psql -U postgres "${database}" -f -
  echo "Done."
}

inject_data $*
