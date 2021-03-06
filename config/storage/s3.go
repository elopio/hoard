package storage

import (
	"fmt"

	"os/user"
	"path"

	"github.com/aws/aws-sdk-go/aws/credentials"
)

type ProviderName string

const (
	EnvProviderName               = "env"
	SharedCredentialsProviderName = "shared"
	StaticProviderName            = "static"
)

type S3Config struct {
	Bucket                   string
	Prefix                   string
	Region                   string
	CredentialsProviderChain []*CredentialsProviderConfig
}

type CredentialsProviderConfig struct {
	Provider string
	*SharedCredentialsProviderConfig
	*StaticProviderConfig
}

// Almost the same a credentials.Value
type StaticProviderConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

func (spc *StaticProviderConfig) Provider() (credentials.Provider, error) {
	if spc == nil {
		return nil, fmt.Errorf("Must include static provider config in " +
			"order to form static provider.")
	}
	return &credentials.StaticProvider{
		Value: credentials.Value{
			AccessKeyID:     spc.AccessKeyID,
			SecretAccessKey: spc.SecretAccessKey,
			SessionToken:    spc.SessionToken,
		},
	}, nil
}

type SharedCredentialsProviderConfig struct {
	Filename string
	Profile  string
}

func (scpc *SharedCredentialsProviderConfig) Provider() (credentials.Provider, error) {
	if scpc == nil {
		return nil, fmt.Errorf("Must include shared credentials provider " +
			"config in order to form shared credentials provider.")
	}
	return &credentials.SharedCredentialsProvider{
		Filename: scpc.Filename,
		Profile:  scpc.Profile,
	}, nil
}

func NewS3Config(addressEncoding, s3Bucket, s3Prefix, region string,
	providers ...credentials.Provider) (*StorageConfig, error) {

	cpConfigs := make([]*CredentialsProviderConfig, 0, len(providers))

	for _, provider := range providers {
		cpConfig, err := ProviderConfig(provider)
		if err != nil {
			return nil, err
		}
		cpConfigs = append(cpConfigs, cpConfig)
	}
	return &StorageConfig{
		StorageType:     S3,
		AddressEncoding: addressEncoding,
		S3Config: &S3Config{
			Bucket: s3Bucket,
			Prefix: s3Prefix,
			Region: region,
			CredentialsProviderChain: cpConfigs,
		},
	}, nil
}

func AWSCredentialsFromChain(cpcs []*CredentialsProviderConfig) (*credentials.Credentials, error) {
	providers := make([]credentials.Provider, 0, len(cpcs))
	var err error
	for _, cpc := range cpcs {
		var provider credentials.Provider
		switch cpc.Provider {
		case EnvProviderName:
			provider = &credentials.EnvProvider{}
		case SharedCredentialsProviderName:
			provider, err = cpc.SharedCredentialsProviderConfig.Provider()
		case StaticProviderName:
			provider, err = cpc.StaticProviderConfig.Provider()
		default:
			err = fmt.Errorf("Credential provider named '%s' could not "+
				"be found", cpc.Provider)
		}

		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}

	var creds *credentials.Credentials

	if len(providers) > 0 {
		creds = credentials.NewChainCredentials(providers)
	}

	return creds, nil
}

func ProviderConfig(provider credentials.Provider) (*CredentialsProviderConfig, error) {
	switch p := provider.(type) {
	case *credentials.EnvProvider:
		return &CredentialsProviderConfig{
			Provider: EnvProviderName,
		}, nil
	case *credentials.SharedCredentialsProvider:
		return &CredentialsProviderConfig{
			Provider: SharedCredentialsProviderName,
			SharedCredentialsProviderConfig: &SharedCredentialsProviderConfig{
				Filename: p.Filename,
				Profile:  p.Profile,
			},
		}, nil
	case *credentials.StaticProvider:
		return &CredentialsProviderConfig{
			Provider: StaticProviderName,
			StaticProviderConfig: &StaticProviderConfig{
				AccessKeyID:     p.AccessKeyID,
				SecretAccessKey: p.SecretAccessKey,
				SessionToken:    p.SessionToken,
			},
		}, nil
	default:
		return nil, fmt.Errorf("Credential provide %s is not recognised", p)
	}
}

func DefaultS3Config() *StorageConfig {
	usr, err := user.Current()
	if err != nil {
		panic(fmt.Errorf("Could not get home directory: %s", err))
	}
	s3c, err := NewS3Config(DefaultAddressEncodingName,
		"monax-hoard-test",
		"store",
		"eu-central-1",
		&credentials.EnvProvider{},
		&credentials.SharedCredentialsProvider{
			Filename: path.Join(usr.HomeDir, ".aws", "credentials"),
			Profile:  "default",
		},
		&credentials.StaticProvider{
			Value: credentials.Value{
				AccessKeyID:     "",
				SecretAccessKey: "",
				SessionToken:    "",
			},
		},
	)
	if err != nil {
		panic(fmt.Errorf("Could not generate example config: %s", err))
	}
	return s3c
}
