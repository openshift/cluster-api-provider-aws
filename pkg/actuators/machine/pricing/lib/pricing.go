package lib

import "github.com/aws/aws-sdk-go/service/pricing"
import "github.com/aws/aws-sdk-go/aws/session"
import "github.com/aws/aws-sdk-go/aws/awserr"
import "github.com/aws/aws-sdk-go/aws"
import "github.com/aws/aws-sdk-go/aws/credentials"
import "fmt"
import "bytes"
import "net/http"
import "io/ioutil"
import "encoding/json"
// import "k8s.io/klog"

type awsAttribute map[string]string
type awsProduct struct {
    Attributes awsAttribute
    ProductFamily string
    Sku string
}

type AwsCreds struct {
	AccessKeyID string
	SecretAccessKey string
}

var (
    attributesToReturn = []string{"gpu", "memory", "vcpu"}
)

func Doit(creds *AwsCreds, instanceType string) map[string]string {
    awsConfig := aws.Config{Region: aws.String("us-east-1")}
    if creds != nil {
        awsConfig.Credentials = credentials.NewStaticCredentials(
            creds.AccessKeyID, creds.SecretAccessKey, "")
    }
    fmt.Println("priciingxxx getting sess")
    // Specify profile for config and region for requests
    sess := session.Must(session.NewSessionWithOptions(session.Options{
         Config: awsConfig,
    }))
    fmt.Println("priciingxxx got sess")
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
                Value: aws.String(instanceType),
            },
        },
        FormatVersion: aws.String("aws_v1"),
        MaxResults:    aws.Int64(1),
    }

    result, err := svc.GetProducts(input)
    if err != nil {
        fmt.Println("priciingxxx", err.Error())
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
        }
        return nil
    }
    product := result.PriceList[0]["product"]
    s, ok := product.(map[string]interface{})
    if ok {
        a, ok2 := s["attributes"].(map[string]interface{})
        if ok2 {
            resMap := make(map[string]string)
            for _, att := range attributesToReturn {
                if val, ok := a[att]; ok {
                    valString, ok := val.(string)
                    if ok {
                        resMap[att] = valString
                    }
                }
            }
            return resMap
        }
    }
    return nil
    // This is a little handier but probably much less efficient
    /*
    jsonString, _ := json.Marshal(product)

    var p awsProduct
    //var p2 awsProduct
    if err := json.Unmarshal(jsonString, &p); err != nil {
        fmt.Println(err)
    }
    fmt.Println(p.Attributes["gpu"])

    */

}

type response struct {
	Products map[string]product `json:"products"`
}

type product struct {
	Attributes productAttributes `json:"attributes"`
}

type productAttributes struct {
	InstanceType string `json:"instanceType"`
	VCPU         string `json:"vcpu"`
	Memory       string `json:"memory"`
	GPU          string `json:"gpu"`
}

// This doesn't work.
func Doit2(instanceType string) {
    url := "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/index.json"
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
                Value: aws.String(instanceType),
            },
        },
        FormatVersion: aws.String("aws_v1"),
        MaxResults:    aws.Int64(1),
    }
    client := http.Client{}
    bodyJson, err := json.Marshal(input)
    fmt.Println(string(bodyJson))
    if err != nil {
        fmt.Println("failed to marshall json")
        return
    }
    req, err := http.NewRequest("GET", url, bytes.NewBuffer(bodyJson))
    // res, err := http.Get(url)
    if err != nil {
        fmt.Println("can't do request")
        return
    }
    res, err := client.Do(req)
    defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println("couldn't parse body")
        return
	}
    var unmarshalled = response{}
    err = json.Unmarshal(body, &unmarshalled)
    if err != nil {
        fmt.Printf("couldn't unmarshall body, %v", err)
        fmt.Println(string(body))
        return
    }
    fmt.Println(unmarshalled)
}

//req, err := http.NewRequest("GET", url, bytes.NewBuffer(bodyJson))
