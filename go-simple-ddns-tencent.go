package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
	"gopkg.in/ini.v1"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
)

// RecordList 定义json数据类型 参考链接：https://mholt.github.io/json-to-go/
type RecordList struct {
	Response struct {
		RecordCountInfo struct {
			ListCount      int `json:"ListCount"`
			SubdomainCount int `json:"SubdomainCount"`
			TotalCount     int `json:"TotalCount"`
		} `json:"RecordCountInfo"`
		RecordList []struct {
			Line          string      `json:"Line"`
			LineID        string      `json:"LineId"`
			Mx            int         `json:"MX"`
			MonitorStatus string      `json:"MonitorStatus"`
			Name          string      `json:"Name"`
			RecordID      int         `json:"RecordId"`
			Remark        string      `json:"Remark"`
			Status        string      `json:"Status"`
			TTL           int         `json:"TTL"`
			Type          string      `json:"Type"`
			UpdatedOn     string      `json:"UpdatedOn"`
			Value         string      `json:"Value"`
			Weight        interface{} `json:"Weight"`
		} `json:"RecordList"`
		RequestID string `json:"RequestId"`
	} `json:"Response"`
}

func main() {
	// 获取配置文件
	cfg, err := ini.Load("config.ini")
	if err != nil {
		fmt.Printf("Fail to read file: %v", err)
		os.Exit(1)
	}
	secretid := cfg.Section("tencent").Key("secretid").String()
	secretkey := cfg.Section("tencent").Key("secretkey").String()
	configSubdomain := cfg.Section("dns").Key("subdomain").String()
	configDomain := cfg.Section("dns").Key("domain").String()
	enableIPv6, _ := cfg.Section("dns").Key("enableIPv6").Bool()
	enable, _ := cfg.Section("").Key("enable").Bool()

	// 获取本机互联网IP
	ipv4, ipv6 := GetPublicIP()

	// 更新记录
	if enable {
		UpdateRecord(secretid, secretkey, configSubdomain, "A", ipv4, configDomain, recordListTencent(secretid, secretkey, configSubdomain, "A", configDomain))
		if len(ipv6) > 0 && enableIPv6 {
			UpdateRecord(secretid, secretkey, configSubdomain, "AAAA", configDomain, ipv6, recordListTencent(secretid, secretkey, configSubdomain, "AAAA", configDomain))

		}
	}
}

func GetPublicIP() (IPv4 string, IPv6 string) {
	// 第一次获取外网 IP，可能获取到IPv6
	responseClient, errClient := http.Get("https://www.cloudflare.com/cdn-cgi/trace")
	if errClient != nil {
		fmt.Printf("获取外网 IP 失败，请检查网络环境\n")
		panic(errClient)
	}
	body, _ := ioutil.ReadAll(responseClient.Body)
	clientIP := fmt.Sprintf("%s", string(body))

	//匹配IPv4
	re := regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+`)
	match := re.FindAllString(clientIP, -1)

	//判断是否返回IPv4
	if len(match) > 0 && len(match[0]) > 1 {
		IPv4 = match[0]
		IPv6 = ""
	} else {
		//如果获取IPv6地址，改用IPv4访问
		re6 := regexp.MustCompile(`(([\da-fA-F]{1,4}):{1,2}){2,8}([\da-fA-F]{1,4})`)
		clientIP := "2408:8214:2820:3870::f6d"
		match := re6.FindAllString(clientIP, -1)
		IPv4 = GetPublicIP4()
		IPv6 = match[0]
	}
	return
}

func GetPublicIP4() (IPv4 string) {
	dialer := &net.Dialer{}
	tr := &http.Transport{
		//强制使用IPv4访问接口地址
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
	}
	client4 := &http.Client{
		Transport: tr,
	}

	responseClient4, errClient := client4.Get("https://www.cloudflare.com/cdn-cgi/trace") // 获取外网 IP
	if errClient != nil {
		fmt.Printf("获取外网 IP 失败，请检查网络\n")
		panic(errClient)
	}

	body, _ := ioutil.ReadAll(responseClient4.Body)
	clientIP := fmt.Sprintf("%s", string(body))

	//print(clientIP)
	re := regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+`)
	match := re.FindAllString(clientIP, -1)
	IPv4 = match[0]
	return
}

func apiCommonTencent(secretid, secretkey string) *dnspod.Client {
	credential := common.NewCredential(
		secretid,
		secretkey,
	)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	client, _ := dnspod.NewClient(credential, "", cpf)
	return client
}

func recordListTencent(secretid, secretkey, SubDomain, RecordType, configDomain string) int {
	//获取域名列表
	requestList := dnspod.NewDescribeRecordListRequest()
	requestList.Domain = common.StringPtr(configDomain)
	responseList, err := apiCommonTencent(secretid, secretkey).DescribeRecordList(requestList)
	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		fmt.Printf("An API error has returned: %s", err)
		return 0
	}
	v := RecordList{}
	err = json.Unmarshal([]byte(responseList.ToJsonString()), &v)
	if err != nil {
		fmt.Println("json解析失败")
	}
	var recordID int
	for _, s := range v.Response.RecordList {
		if s.Name == SubDomain && s.Type == RecordType {
			recordID = s.RecordID
		}
	}
	return recordID
}

func UpdateRecord(secretid, secretkey, SubDomain, RecordType, Value, configDomain string, RecordId int) {
	//更新单个记录
	requestRecord := dnspod.NewModifyRecordRequest()
	requestRecord.Domain = common.StringPtr(configDomain)
	requestRecord.SubDomain = common.StringPtr(SubDomain)
	requestRecord.RecordType = common.StringPtr(RecordType)
	requestRecord.RecordLine = common.StringPtr("默认")
	requestRecord.RecordLineId = common.StringPtr("0")
	requestRecord.RecordId = common.Uint64Ptr(uint64(RecordId))
	requestRecord.Value = common.StringPtr(Value)

	responseRecord, err := apiCommonTencent(secretid, secretkey).ModifyRecord(requestRecord)
	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		fmt.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Println(responseRecord.ToJsonString())
}