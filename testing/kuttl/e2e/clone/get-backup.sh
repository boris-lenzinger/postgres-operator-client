#!/bin/bash

p_cluster_name=$1
p_namespace=$2
backup_nb=$3

backup_count=$(kubectl-pgo show backup ${p_cluster_name} -n ${p_namespace} |\
  grep "timestamp start/stop" |\
  sed -e "s#^.* / ##" |\
  wc -l)

tail_count=$(( backup_count - backup_nb + 1 ))

kubectl-pgo show backup ${p_cluster_name} -n ${p_namespace} |\
  grep "timestamp start/stop" |\
  sed -e "s#^.* / ##" |\
  tail -n${tail_count} |\
  head -n1

