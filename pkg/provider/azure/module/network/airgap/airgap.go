package airgap

import (
	"github.com/pulumi/pulumi-azure-native-sdk/network/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	maptContext "github.com/redhat-developer/mapt/pkg/manager/context"
	resourcesUtil "github.com/redhat-developer/mapt/pkg/util/resources"
)

const (
	cidrVN = "10.0.0.0/16"
	cidrSN = "10.0.2.0/24"
)

type NetworkRequest struct {
	Prefix        string
	ComponentID   string
	ResourceGroup *resources.ResourceGroup
	CIDR          string
	Name          string
	Region        string

	AvailabilityZone string
	// This subnet is will be created first as private
	// to orchestrate on a 2nd phase a param will remove the
	// nat gateway
	TargetSubnetCIDR string
	PublicSubnetCIDR string
	SetAsAirgap      bool
}

func (s NetworkRequest) CreateNetwork(ctx *pulumi.Context) (*network.VirtualNetwork, *network.Subnet, *network.PublicIPAddress, *network.NetworkInterface, error) {
	vn, err := network.NewVirtualNetwork(ctx,
		resourcesUtil.GetResourceName(s.Prefix, s.ComponentID, "vn"),
		&network.VirtualNetworkArgs{
			VirtualNetworkName: pulumi.String(maptContext.RunID()),
			AddressSpace: network.AddressSpaceArgs{
				AddressPrefixes: pulumi.StringArray{
					pulumi.String(cidrVN),
				},
			},
			ResourceGroupName: s.ResourceGroup.Name,
			Location:          s.ResourceGroup.Location,
			Tags:              maptContext.ResourceTags(),
		})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sn, err := network.NewSubnet(ctx,
		resourcesUtil.GetResourceName(s.Prefix, s.ComponentID, "sn"),
		&network.SubnetArgs{
			SubnetName:         pulumi.String(maptContext.RunID()),
			ResourceGroupName:  s.ResourceGroup.Name,
			VirtualNetworkName: vn.Name,
			AddressPrefixes: pulumi.StringArray{
				pulumi.String(cidrSN),
			},
		})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	publicIP, err := network.NewPublicIPAddress(ctx,
		resourcesUtil.GetResourceName(s.Prefix, s.ComponentID, "pip"),
		&network.PublicIPAddressArgs{
			Location:                 s.ResourceGroup.Location,
			PublicIpAddressName:      pulumi.String(maptContext.RunID()),
			PublicIPAllocationMethod: pulumi.String("Static"),
			ResourceGroupName:        s.ResourceGroup.Name,
			Tags:                     maptContext.ResourceTags(),
			// DnsSettings: network.PublicIPAddressDnsSettingsArgs{
			// 	DomainNameLabel: pulumi.String("mapt"),
			// },
		})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	ni, err := network.NewNetworkInterface(ctx,
		resourcesUtil.GetResourceName(s.Prefix, s.ComponentID, "ni"),
		&network.NetworkInterfaceArgs{
			NetworkInterfaceName: pulumi.String(maptContext.RunID()),
			Location:             s.ResourceGroup.Location,
			ResourceGroupName:    s.ResourceGroup.Name,
			IpConfigurations: network.NetworkInterfaceIPConfigurationArray{
				&network.NetworkInterfaceIPConfigurationArgs{
					Name:                      pulumi.String(maptContext.RunID()),
					PrivateIPAllocationMethod: pulumi.String("Dynamic"),
					PublicIPAddress: network.PublicIPAddressTypeArgs{
						Id: publicIP.ID(),
					},
					Subnet: network.SubnetTypeArgs{
						Id: sn.ID(),
					},
				},
			},
			Tags: maptContext.ResourceTags(),
		})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return vn, sn, publicIP, ni, nil
}
