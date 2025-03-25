package network

import (
	"github.com/pulumi/pulumi-azure-native-sdk/network/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	airGappedNetwork "github.com/redhat-developer/mapt/pkg/provider/azure/module/network/airgap"
	standardNetwork "github.com/redhat-developer/mapt/pkg/provider/azure/module/network/standard"
)

type Connectivity int

const (
	ON Connectivity = iota
	OFF
)

type NetworkRequest struct {
	Prefix                  string
	ComponentID             string
	ResourceGroup           *resources.ResourceGroup
	Airgap                  bool
	AirgapPhaseConnectivity Connectivity
}

type Network struct {
	Network          *network.VirtualNetwork
	PublicSubnet     *network.Subnet
	NetworkInterface *network.NetworkInterface
	PublicIP         *network.PublicIPAddress
}

// Create networking resource required for spin the VM
func (r *NetworkRequest) Create(ctx *pulumi.Context) (*Network, error) {
	var virtualNetwork *network.VirtualNetwork
	var targetSubnet *network.Subnet
	var publicIP *network.PublicIPAddress
	var networkInterface *network.NetworkInterface
	var err error
	if r.Airgap {
		virtualNetwork, targetSubnet, publicIP, networkInterface, err = airGappedNetwork.NetworkRequest{
			Prefix:        r.Prefix,
			ComponentID:   r.ComponentID,
			ResourceGroup: r.ResourceGroup,
		}.CreateNetwork(ctx)

	} else {
		virtualNetwork, targetSubnet, publicIP, networkInterface, err = standardNetwork.NetworkRequest{
			Prefix:        r.Prefix,
			ComponentID:   r.ComponentID,
			ResourceGroup: r.ResourceGroup,
		}.CreateNetwork(ctx)
		if err != nil {
			return nil, err
		}
	}
	return &Network{
		NetworkInterface: networkInterface,
		PublicIP:         publicIP,
		Network:          virtualNetwork,
		PublicSubnet:     targetSubnet,
	}, nil
}
