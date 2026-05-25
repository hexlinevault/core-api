package configs

import "github.com/aws/aws-sdk-go-v2/aws"

// S3Conn s3 configuretion
type S3Conn struct {
	aws.Config
	ConnectionName string // empty is default
}
