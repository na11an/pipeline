AWSTemplateFormatVersion: 2010-09-09
Description: 'Worker Node(s) for Banzai Cloud Pipeline Kubernetes Engine'
Parameters:
  SSHLocation:
    Description: The IP address range that can be used to SSH to the EC2 instances
    Type: String
    MinLength: '9'
    MaxLength: '18'
    Default: 0.0.0.0/0
    AllowedPattern: '(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})/(\d{1,2})'
    ConstraintDescription: must be a valid IP CIDR range of the form x.x.x.x/x.
  InstanceType:
    Description: EC2 instance type(s)
    Type: String
    ConstraintDescription: must be a valid EC2 instance type.
  ClusterName:
    Description: PKE Cluster name
    Type: String
  AvailabilityZones:
    Type: 'List<AWS::EC2::AvailabilityZone::Name>'
    Description: Specify Availability Zones for Autoscaling
  VPCId:
    Type: 'AWS::EC2::VPC::Id'
    Description: Specify VPC Id for Autoscaling
  SubnetIds:
    Type: 'List<AWS::EC2::Subnet::Id>'
    Description: Specify Subnet Id for Autoscaling
  GlobalStack:
    Type: String
    Description: Specify Stack for Role(s) and InstanceProfiles(s)
  MasterStack:
    Type: String
    Description: Specify Stack for Role(s) and InstanceProfiles(s)
  KubernetesApiServer:
    Type: String
    Description: "Kubernetes API Server host port. example: 10.0.0.1:6443"
  PipelineUrl:
    Type: String
  PipelineToken:
    Type: String
  PipelineOrgId:
    Type: String
  PipelineClusterId:
    Type: String
  PipelineNodePool:
    Type: String
    Default: "pool1"
  KubernetesPodNetworkCidr:
    Type: String
  KeyName:
    Type: 'AWS::EC2::KeyPair::KeyName'
    Description: Name of an existing EC2 KeyPair to enable SSH access to the instance
    Default: ""
Resources:
  LaunchConfiguration:
    Type: AWS::AutoScaling::LaunchConfiguration
    Properties:
      KeyName: !Ref KeyName
      InstanceType: !Ref InstanceType
      ImageId: !FindInMap [RegionMap, !Ref "AWS::Region", ami]
      IamInstanceProfile: {'Fn::ImportValue': !Sub '${GlobalStack}-WorkerInstanceProfile'}
      AssociatePublicIpAddress: true
      SecurityGroups:
      - !Ref SecurityGroup
      - {'Fn::ImportValue': !Sub '${MasterStack}-ClusterSecurityGroup'}
      BlockDeviceMappings:
      - DeviceName: /dev/sda1
        Ebs:
          VolumeSize: '50'
      UserData:
        Fn::Base64:
          Fn::Sub:
          - |
            #!/usr/bin/env bash
            set -e
            export SIGNAL_URL="${SignalUrl}"
            export PUBLICIP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)

            curl -v https://banzaicloud.com/downloads/pke/banzai-cluster-pke-0.0.8 -o /usr/local/bin/pke-installer
            chmod +x /usr/local/bin/pke-installer

            /usr/local/bin/pke-installer install worker \
            --pipeline-url=${PipelineUrl} \
            --pipeline-token="${PipelineToken}" \
            --pipeline-org-id=${PipelineOrgId} \
            --pipeline-cluster-id=${PipelineClusterId} \
            --pipeline-nodepool=${PipelineNodePool} \
            --kubernetes-cloud-provider=aws \
            --kubernetes-infrastructure-cidr=${KubernetesPodNetworkCidr}

            curl -X PUT -H 'Content-Type: ' --data-binary "{\"Status\":\"SUCCESS\",\"Reason\":\"Configuration Complete\",\"UniqueId\":\"$(date +%s)\"}" $SIGNAL_URL
          - {
              SignalUrl: !Ref WaitForFirstInstanceHandle,
              AwsRegion: !Ref 'AWS::Region',
              PipelineUrl: !Ref PipelineUrl,
              PipelineToken: !Ref PipelineToken,
              PipelineOrgId: !Ref PipelineOrgId,
              PipelineClusterId: !Ref PipelineClusterId,
              PipelineNodePool: !Ref PipelineNodePool,
            }
  AutoScalingGroup:
    Type: AWS::AutoScaling::AutoScalingGroup
    Properties:
      AvailabilityZones: !Ref AvailabilityZones
      LaunchConfigurationName:
        Ref: LaunchConfiguration
      DesiredCapacity: '1'
      MinSize: "1"
      MaxSize: "1"
      VPCZoneIdentifier: !Ref SubnetIds
      Tags:
      - Key: ClusterName
        Value: !Ref ClusterName
        PropagateAtLaunch: True
      - Key: Name
        Value: !Join ["", ["pke-worker"]]
        PropagateAtLaunch: True
      - Key: !Join [ "", [ "kubernetes.io/cluster/", !Ref ClusterName] ]
        Value: "owned"
        PropagateAtLaunch: True

  SecurityGroup:
    Type: 'AWS::EC2::SecurityGroup'
    Properties:
      GroupDescription: 'Enable SSH via port 22'
      VpcId:
        Ref: VPCId
      SecurityGroupIngress:
      - IpProtocol: tcp
        FromPort: '22'
        ToPort: '22'
        CidrIp: !Ref SSHLocation
      - IpProtocol: -1
        SourceSecurityGroupId: {'Fn::ImportValue': !Sub '${MasterStack}-ClusterSecurityGroup'}
      Tags:
      - Key: Name
        Value: !Join ["", ["pke-worker-sg-",!Ref "AWS::StackName"]]

  WaitForFirstInstance:
    Type: AWS::CloudFormation::WaitCondition
    DependsOn: AutoScalingGroup
    Properties:
      Handle:
        Ref: "WaitForFirstInstanceHandle"
      Timeout: 6000

  WaitForFirstInstanceHandle:
    Type: AWS::CloudFormation::WaitConditionHandle