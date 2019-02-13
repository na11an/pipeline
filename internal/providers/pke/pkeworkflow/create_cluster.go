// Copyright © 2019 Banzai Cloud
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

package pkeworkflow

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
	pkgCluster "github.com/banzaicloud/pipeline/cluster"
	"github.com/banzaicloud/pipeline/internal/backoff"
	"github.com/banzaicloud/pipeline/internal/cluster"
	pkgSecret "github.com/banzaicloud/pipeline/pkg/secret"
	"github.com/banzaicloud/pipeline/secret"
	"github.com/goph/emperror"
	"go.uber.org/cadence/workflow"
	"go.uber.org/zap"
	"io/ioutil"
)

const CreateClusterWorkflowName = "pke-create-cluster"

type CreateClusterWorkflowInput struct {
	ClusterID uint
}

func CreateClusterWorkflow(ctx workflow.Context, input uint) error {
	ao := workflow.ActivityOptions{
		ScheduleToStartTimeout: 5 * time.Minute,
		StartToCloseTimeout:    5 * time.Minute,
		WaitForCancellation:    true,
	}

	ctx = workflow.WithActivityOptions(ctx, ao)

	createClusterActivityInput := CreateClusterActivityInput{
		ClusterID: input,
	}

	err := workflow.ExecuteActivity(ctx, CreateClusterActivityName, createClusterActivityInput).Get(ctx, nil)
	if err != nil {

		// NOOOOOOO
		//return nil
		return err
	}

	signalName := "master-ready"
	signalChan := workflow.GetSignalChannel(ctx, signalName)

	s := workflow.NewSelector(ctx)
	s.AddReceive(signalChan, func(c workflow.Channel, more bool) {
		c.Receive(ctx, nil)
		workflow.GetLogger(ctx).Info("Received signal!", zap.String("signal", signalName))
	})
	s.Select(ctx)

	return nil
}

const CreateClusterActivityName = "pke-create-cluster-activity"

type CreateClusterActivity struct {
	clusterManager *pkgCluster.Manager
	tokenGenerator TokenGenerator
}

func NewCreateClusterActivity(clusterManager *pkgCluster.Manager, tokenGenerator TokenGenerator) *CreateClusterActivity {
	return &CreateClusterActivity{
		clusterManager: clusterManager,
		tokenGenerator: tokenGenerator,
	}
}

type TokenGenerator interface {
	GenerateClusterToken(orgID, clusterID uint) (string, string, error)
}

type CreateClusterActivityInput struct {
	ClusterID uint
}

func CheckPkeGlobalCF(cloudformationSrv *cloudformation.CloudFormation) (bool, error) {
	stackFilter := cloudformation.ListStacksInput{
		StackStatusFilter: aws.StringSlice([]string{"CREATE_COMPLETE", "CREATE_IN_PROGRESS"}),
	}
	stacks, err := cloudformationSrv.ListStacks(&stackFilter)
	if err != nil {
		return false, err
	}
	for _, stack := range stacks.StackSummaries {
		fmt.Printf("%#v", stack)
		if *stack.StackName == "pke-global" {
			return true, nil
		}
	}
	return false, nil
}

func (a *CreateClusterActivity) Execute(ctx context.Context, input CreateClusterActivityInput) error {
	c, err := a.clusterManager.GetClusterByIDOnly(ctx, input.ClusterID)
	if err != nil {
		return err
	}
	cc := c.(*pkgCluster.EC2ClusterPKE)
	// Generate certificates
	clusterUidTag := fmt.Sprintf("clusterUID:%s", c.GetUID())
	req := &secret.CreateSecretRequest{
		Name: fmt.Sprintf("cluster-%d-ca", c.GetID()),
		Values: map[string]string{
			pkgSecret.ClusterUID: fmt.Sprintf("%d-%d", c.GetOrganizationId(), c.GetID()),
		},
		Type: pkgSecret.PKESecretType,
		Tags: []string{
			clusterUidTag,
			pkgSecret.TagBanzaiReadonly,
			pkgSecret.TagBanzaiHidden,
		},
	}
	_, err = secret.Store.GetOrCreate(c.GetOrganizationId(), req)
	if err != nil {
		return err
	}
	// prepare input for real AWS flow
	_, _, err = a.tokenGenerator.GenerateClusterToken(c.GetOrganizationId(), c.GetID())
	if err != nil {
		return emperror.Wrap(err, "can't generate Pipeline token")
	}

	client, err := cc.GetAWSClient()
	if err != nil {
		panic("invalid aws credential")
	}
	stsSrv := sts.New(client)
	identityOutput, err := stsSrv.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%#v", identityOutput)
	cloudformationSrv := cloudformation.New(client)
	ok, err := CheckPkeGlobalCF(cloudformationSrv)
	if err != nil {
		panic(err)
	}
	if !ok {
		buf, err := ioutil.ReadFile("templates/global.cf.tpl")
		if err != nil {
			panic(err)
		}
		stackInput := &cloudformation.CreateStackInput{
			Capabilities: aws.StringSlice([]string{"CAPABILITY_IAM", "CAPABILITY_NAMED_IAM"}),
			StackName:    aws.String("pke-global"),
			TemplateBody: aws.String(string(buf)),
		}
		output, err := cloudformationSrv.CreateStack(stackInput)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%#v", output)
	}
	//TODO wait for CREATE_COMPLETE
	stackOutput, err := cloudformationSrv.DescribeStacks(&cloudformation.DescribeStacksInput{
		StackName: aws.String("pke-global"),
	})
	var workerInstanceProfile string
	var masterInstanceProfile string
	for _, stack := range stackOutput.Stacks {
		fmt.Printf("%#v", stack.Outputs)
		for _, o := range stack.Outputs {
			if *o.OutputKey == "WorkerInstanceProfile" {
				workerInstanceProfile = *o.OutputValue
			}
			if *o.OutputKey == "MasterInstanceProfile" {
				masterInstanceProfile = *o.OutputValue
			}
		}
	}
	fmt.Println(workerInstanceProfile)
	fmt.Println(masterInstanceProfile)
	// EIP
	ec2Srv := ec2.New(client)
	eipOut, err := ec2Srv.AllocateAddress(&ec2.AllocateAddressInput{})
	if err != nil {
		panic(err)
	}
	// Save to DB
	fmt.Println(*eipOut.PublicIp)
	// Create VPC
	buf, err := ioutil.ReadFile("templates/vpc.cf.tpl")
	if err != nil {
		panic(err)
	}
	vpcStackInput := &cloudformation.CreateStackInput{
		Capabilities: aws.StringSlice([]string{""}),
		StackName:    aws.String("pke-master-" + cc.model.Cluster.Name),
		TemplateBody: aws.String(string(buf)),
		Parameters: []*cloudformation.Parameter{
			{
				ParameterKey:   aws.String("ClusterName"),
				ParameterValue: aws.String(cc.model.Cluster.Name),
			},
		},
	}
	_, err = cloudformationSrv.CreateStack(vpcStackInput)
	if err != nil {
		panic(err)
	}
	//stackOutput, err := cloudformationSrv.DescribeStacks(&cloudformation.DescribeStacksInput{
	//	StackName: aws.String("pke-master-" + ClusterName),
	//})
	//fmt.Println(stackOutput.)

	//masterStackInput := &cloudformation.CreateStackInput{
	//	Capabilities: aws.StringSlice([]string{""}),
	//	StackName: aws.String("pke-master-" + ClusterName),
	//	TemplateBody: aws.String(string(buf)),
	//	Parameters: []*cloudformation.Parameter{
	//		{
	//			ParameterKey: aws.String("InstanceType"),
	//			ParameterValue: aws.String("m4.xlarge"),
	//		},
	//		{
	//			ParameterKey: aws.String("ClusterName"),
	//			ParameterValue: aws.String(ClusterName),
	//		},
	//		{
	//			ParameterKey: aws.String("AvailabilityZone"),
	//			ParameterValue: aws.String(""),
	//		},
	//		{
	//			ParameterKey: aws.String("VPCId"),
	//			ParameterValue: aws.String(""),
	//		},
	//		{
	//			ParameterKey: aws.String("SubnetId"),
	//			ParameterValue: aws.String(""),
	//		},
	//		{
	//			ParameterKey: aws.String("EIPAllocationId"),
	//			ParameterValue: eipOut.AllocationId,
	//		},
	//	},
	//}
	//masterStackOutput, err := cloudformationSrv.CreateStack(masterStackInput)
	//fmt.Println(masterStackOutput)

	token := "XXX" // TODO masked from dumping valid tokens to log
	for _, nodePool := range cc.model.NodePools {
		cmd := cc.GetBootstrapCommand(nodePool.Name, externalBaseURL, token)
		cc.log.Debugf("TODO: start ASG with command %s", cmd)
	}

	c.UpdateStatus(c.model.Cluster.Status, "Waiting for Kubeconfig from master node.")
	clusters := cluster.NewClusters(pipConfig.DB()) // TODO get it from non-global context
	err = backoff.Retry(func() error {
		id, err := clusters.GetConfigSecretIDByClusterID(c.GetOrganizationId(), c.GetID())
		if err != nil {
			return err
		} else if id == "" {
			log.Debug("waiting for Kubeconfig (/ready call)")
			return errors.New("no Kubeconfig received from master")
		}
		cc.model.Cluster.ConfigSecretID = id
		return nil
	}, backoff.NewConstantBackoffPolicy(&backoff.ConstantBackoffConfig{
		Delay:          20 * time.Second,
		MaxElapsedTime: 30 * time.Minute}))

	if err != nil {
		return emperror.Wrap(err, "timeout")
	}

	c.UpdateStatus("CREATING", "Waiting for Kubeconfig from master node.")

	return nil
}
