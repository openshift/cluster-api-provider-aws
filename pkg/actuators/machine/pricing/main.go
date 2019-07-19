package main

import "sigs.k8s.io/cluster-api-provider-aws/pkg/actuators/machine/pricing/lib"
import "fmt"
//import "io/ioutil"
// import "encoding/json"

type awsAttribute map[string]string
type awsProduct struct {
    Attributes awsAttribute
    ProductFamily string
    Sku string
}

var (
    attributesToReturn = []string{"gpu", "memory", "vcpu"}
)

func main() {
    resMap := lib.Doit(nil, "p2.16xlarge")
    fmt.Println(resMap)

    // lib.Doit2("p2.8xlarge")
}
