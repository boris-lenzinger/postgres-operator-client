// Copyright 2021 - 2023 Crunchy Data Solutions, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal"
	"github.com/crunchydata/postgres-operator-client/internal/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/crunchydata/postgres-operator-client/internal/processing"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"strings"
)

var replicaCountValue int
var allClusters bool

// newReplicaCountCommand returns the delete subcommand of the PGO plugin.
// Subcommands of replica-count will be used to update the replica field of clusters.
func newReplicaCountCommand(config *internal.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replica-count",
		Short: "Upgrades the number of replicas (including the master) to a given value. 0 and negative values are rejected.",
		Long:  "Upgrades the number of replicas (including the master) to a given value. 0 and negative values are rejected.",
	}

	cmd.AddCommand(newReplicaCountClusterCommand(config))

	return cmd
}

// newReplicaCountClusterCommand returns the delete cluster subcommand.
// delete cluster will take a cluster name as an argument
func newReplicaCountClusterCommand(config *internal.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replica-count postgrescluster CLUSTER_NAME <number>",
		Short: "Update the replica count of the cluster. If no cluster is supplied and option --all is passed, all clusters will have their replica count updated.",
		Long: `Update the replica count of the cluster. If no cluster is supplied and option --all is passed, all clusters will have their replica count updated.

#### RBAC Requirements
    Resources                                           Verbs
    ---------                                           -----
    postgresclusters.postgres-operator.crunchydata.com  [get,list,update]`,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if replicaCountValue <= 0 || replicaCountValue > 4 {
			return fmt.Errorf(fmt.Sprintf("Value for replica must be between 1 and 4. You supplied %d", replicaCountValue))
		}
		if len(args) == 0 && !allClusters {
			return fmt.Errorf("you did not supply a cluster name and you did not use option --all. Cannot guess which cluster should be updated")
		}

		ctx := context.Background()

		var clustersList []unstructured.Unstructured

		_, clientCrunchy, err := v1beta1.NewPostgresClusterClient(config)
		if err != nil {
			return err
		}

		namespace := *config.ConfigFlags.Namespace

		if allClusters {
			l, err := clientCrunchy.Namespace("").List(ctx, metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("failed to list all crunchy clusters due to %s", err.Error())
			}
			clustersList = l.Items
		} else {
			clusterName := args[0]
			clusterToUpdate, err := clientCrunchy.Namespace(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get cluster %q in namespace %q due to %s", clusterName, namespace, err.Error())
			}
			clustersList = []unstructured.Unstructured{*clusterToUpdate}
		}

		_, restConfig, err := processing.ConfigureK8sClient(config.ConfigFlags)
		if err != nil {
			return err
		}

		client, err := dynamic.NewForConfig(restConfig)
		if err != nil {
			return err
		}

		pgClusterResource := schema.GroupVersionResource{Version: "v1beta1", Group: "postgres-operator.crunchydata.com", Resource: "postgresclusters"}

		fmt.Printf("Changing replica count to %d for %d clusters\n", replicaCountValue, len(clustersList))
		var errOccured bool
		var errorsPerCluster []string
		for i, c := range clustersList {
			fmt.Printf("(%d/%d) %s / %s \n", i+1, len(clustersList), c.GetNamespace(), c.GetName())
			c = changeReplicaTo(c, replicaCountValue)
			_, err = client.Resource(pgClusterResource).
				Namespace(c.GetNamespace()).
				Update(context.TODO(), &c, metav1.UpdateOptions{})
			if err != nil {
				errOccured = true
				errorsPerCluster = append(errorsPerCluster, fmt.Sprintf("%s / %s : %s", c.GetNamespace(), c.GetName(), err.Error()))
			}
		}
		if errOccured {
			return fmt.Errorf("failed to update at least one cluster. See below details \n - %s", strings.Join(errorsPerCluster, "\n - "))
		}

		return nil
	}

	// kubectl-pgo replica-count clustername --value 3
	// kubectl-pgo replica-count --all
	cmd.Flags().IntVar(&replicaCountValue, "value", 2, "Set the number of nodes in a postgres cluster. 0 and below are rejected. Above 4 is rejected too.")
	cmd.Flags().BoolVar(&allClusters, "all", false, "Is used to require to modify all clusters.")

	return cmd
}

func changeReplicaTo(crunchyCluster unstructured.Unstructured, replicaCount int) unstructured.Unstructured {
	spec := crunchyCluster.Object["spec"].(map[string]interface{})
	instances := spec["instances"].([]interface{})
	instance0 := instances[0].(map[string]interface{})
	instance0["replicas"] = replicaCount
	instances[0] = instance0
	spec["instances"] = instances
	crunchyCluster.Object["spec"] = spec

	return crunchyCluster
}
