package bootstrap

import (
	"context"

	"github.com/hexlinevault/core-api.git/configs"

	manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

var s3session map[string]*s3.Client = make(map[string]*s3.Client)

type S3 struct {
}

// InitS3 initial s3 config
//
//	config := &configs.S3Conn{
//	 Config: aws.Config{
//		 Region: aws.String(os.Getenv("AWS_REGION")),
//	 }
//	}
func CreateS3Session(conf *configs.S3Conn) {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})
	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "s3").Info("S3 bucket connected")
	s3Client := s3.NewFromConfig(conf.Config)
	s3session[connectionName] = s3Client
}

func (ctl *S3) Session(connectionNames ...string) *s3.Client {
	connectionName := resolveConnectionName(connectionNames)
	return s3session[connectionName]
}

func (ctl *S3) Uploader(connectionNames ...string) *manager.Uploader {
	connectionName := resolveConnectionName(connectionNames)
	return manager.NewUploader(s3session[connectionName])
}

func (ctl *S3) Downloader(connectionNames ...string) *manager.Downloader {
	connectionName := resolveConnectionName(connectionNames)
	return manager.NewDownloader(s3session[connectionName])
}

func (ctl *S3) Service(connectionNames ...string) *s3.Client {
	connectionName := resolveConnectionName(connectionNames)
	return s3.New(s3session[connectionName].Options())
}
