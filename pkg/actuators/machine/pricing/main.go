package main

import "github.com/aws/aws-sdk-go/service/pricing"
import "github.com/aws/aws-sdk-go/aws/session"
import "github.com/aws/aws-sdk-go/aws/awserr"
import "github.com/aws/aws-sdk-go/aws"
import "fmt"

func main() {
    // Specify profile for config and region for requests
    sess := session.Must(session.NewSessionWithOptions(session.Options{
         Config: aws.Config{Region: aws.String("us-east-1")},
    }))
    svc := pricing.New(sess)
    input := &pricing.GetProductsInput{
        ServiceCode: aws.String("AmazonEC2"),
        Filters: []*pricing.Filter{
            {
                Field: aws.String("productFamily"),
                Type:  aws.String("TERM_MATCH"),
                Value: aws.String("Compute Instance"),
            },
            {
                Field: aws.String("instanceType"),
                Type:  aws.String("TERM_MATCH"),
                Value: aws.String("r4.8xlarge"),
            },
        },
        FormatVersion: aws.String("aws_v1"),
        MaxResults:    aws.Int64(1),
    }

    result, err := svc.GetProducts(input)
    if err != nil {
        if aerr, ok := err.(awserr.Error); ok {
            switch aerr.Code() {
            case pricing.ErrCodeInternalErrorException:
                fmt.Println(pricing.ErrCodeInternalErrorException, aerr.Error())
            case pricing.ErrCodeInvalidParameterException:
                fmt.Println(pricing.ErrCodeInvalidParameterException, aerr.Error())
            case pricing.ErrCodeNotFoundException:
                fmt.Println(pricing.ErrCodeNotFoundException, aerr.Error())
            case pricing.ErrCodeInvalidNextTokenException:
                fmt.Println(pricing.ErrCodeInvalidNextTokenException, aerr.Error())
            case pricing.ErrCodeExpiredNextTokenException:
                fmt.Println(pricing.ErrCodeExpiredNextTokenException, aerr.Error())
            default:
                fmt.Println(aerr.Error())
            }
        } else {
            // Print the error, cast err to awserr.Error to get the Code and
            // Message from an error.
            fmt.Println(err.Error())
        }
        return
    }
    product := result.PriceList[0]["product"]
    fmt.Println(product)

}
