package helpers

import (
	"bytes"
	"fmt"
	"html/template"
)

func ParseTemplate(temp *template.Template, data interface{}) (*bytes.Buffer, error) {
	var body bytes.Buffer
	if err := temp.Execute(&body, data); err != nil {
		fmt.Printf("Load mail template error: %s\n", err.Error())
		return nil, err
	}
	return &body, nil
}
