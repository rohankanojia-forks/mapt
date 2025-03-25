package bastion

import (
	"github.com/pulumi/pulumi-azure-native-sdk/compute/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/network/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi-tls/sdk/v5/go/tls"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/redhat-developer/mapt/pkg/provider/azure/module/keypair"
)

const (
	imageRegex = "ubuntu-image"
)

type Request struct {
	Prefix              string
	VPC                 *network.VirtualNetwork
	Subnet              *network.Subnet
	ResourceGroup       *resources.ResourceGroup
	OutputKeyPrivateKey string
	OutputKeyHost       string
	OutputKeyUsername   string
}

type Bastion struct {
	PrivateKey tls.PrivateKey
	Username   string
	Port       int
}

func (b *Request) Create(ctx *pulumi.Context) string {
	// GET IMAGE

	//Create Keypair
	privateKeyPEM, publicKeySSH, err := keypair.GenerateSSHKeyPair(4096)
	if err != nil {
		return ""
	}

	// export keypair to context
	ctx.Export(privateKeyPEM, publicKeySSH)

	// create instance
	// return
}
