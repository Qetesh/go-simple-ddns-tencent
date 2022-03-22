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
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
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

func init() {
	// 初始化日志配置
	logFile, err := os.OpenFile("./ddns.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.ModeAppend|os.ModePerm)
	if err != nil {
		log.Println("open log file failed, err:", err)
		return
	}
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	// 获取配置文件
	cfg, err := ini.Load("config.ini")
	if err != nil {
		log.Printf("Fail to read file: %v", err)
		os.Exit(1)
	}
	secretid := cfg.Section("tencent").Key("secretid").String()
	secretkey := cfg.Section("tencent").Key("secretkey").String()
	configSubdomain := cfg.Section("dns").Key("subdomain").String()
	configDomain := cfg.Section("dns").Key("domain").String()
	enableIPv6, _ := cfg.Section("dns").Key("enableIPv6").Bool()
	enable, _ := cfg.Section("").Key("enable").Bool()

	if enable {
		// 获取本机互联网IP
		CurrentIPv4, CurrentIPv6 := GetPublicIP()
		log.Println("当前互联网IP：", CurrentIPv4, CurrentIPv6)
		var MatchIPv4 bool
		var MatchIPv6 bool

		// 判断IP是否变更
		ResolveIPv4, ResolveIPv6 := Resovle(configDomain, configSubdomain)
		log.Println("解析互联网IP：", ResolveIPv4, ResolveIPv6)

		if CurrentIPv4 == ResolveIPv4 {
			MatchIPv4 = true
		}
		if CurrentIPv6 == ResolveIPv6 {
			MatchIPv6 = true
		}

		log.Println("MatchIPv4:", MatchIPv4, "MatchIPv6:", MatchIPv6)
		if MatchIPv4 && MatchIPv6 {
			log.Println("域名未变化")
		} else {
			if len(ResolveIPv6) == 0 && len(CurrentIPv6) > 0 && enableIPv6 {
				// 添加IPv6
				TencentCreateRecord(configDomain, configSubdomain, "AAAA", CurrentIPv6, secretid, secretkey)
			}
			if len(ResolveIPv4) == 0 {
				// 添加IPv4
				TencentCreateRecord(configDomain, configSubdomain, "A", CurrentIPv4, secretid, secretkey)
			}
			if !enableIPv6 || (len(ResolveIPv6) > 0 && len(CurrentIPv6) == 0) {
				// 删除IPv6
				TencentDelRecord(configDomain, secretid, secretkey, TencentDomainID(secretid, secretkey, configSubdomain, "AAAA", configDomain))
			}
			if !MatchIPv4 && !(len(ResolveIPv4) == 0) {
				// 更新IPv4
				TencentUpdateRecord(secretid, secretkey, configSubdomain, "A", "ENABLE", CurrentIPv4, configDomain, TencentDomainID(secretid, secretkey, configSubdomain, "A", configDomain))
			}
			if !MatchIPv6 && !(len(CurrentIPv6) == 0) && enableIPv6 {
				// 更新IPv6
				TencentUpdateRecord(secretid, secretkey, configSubdomain, "AAAA", "ENABLE", CurrentIPv4, configDomain, TencentDomainID(secretid, secretkey, configSubdomain, "AAAA", configDomain))
			}
		}
	}
}

func GetPublicIP() (IPv4 string, IPv6 string) {
	// 第一次获取外网 IP，可能获取到IPv6
	responseClient, errClient := http.Get("https://www.cloudflare.com/cdn-cgi/trace")
	if errClient != nil {
		log.Printf("获取外网 IP 失败，请检查网络环境\n")
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
		match := re6.FindAllString(clientIP, -1)
		IPv4 = GetPublicIPv4()
		IPv6 = match[0]
	}
	return
}

func GetPublicIPv4() (IPv4 string) {
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
		log.Printf("获取外网 IP 失败，请检查网络\n")
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

func Resovle(configdomain, configSubdomain string) (ResolveIPv4 string, ResolveIPv6 string) {
	record, _ := net.LookupIP(configSubdomain + "." + configdomain)

	for _, ip := range record {
		re := regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
		match := re.FindAllString(ip.String(), -1)

		//判断是否返回IPv4
		if len(match) > 0 && len(match[0]) > 1 {
			ResolveIPv4 = match[0]
			//ResolveIPv6 = ""
		} else {
			//获取IPv6地址
			re6 := regexp.MustCompile(`(([\da-fA-F]{1,4}):{1,2}){2,8}([\da-fA-F]{1,4})`)
			match6 := re6.FindAllString(ip.String(), -1)
			ResolveIPv6 = match6[0]
		}
	}
	return
}

func TencentApiCommon(secretid, secretkey string) *dnspod.Client {
	credential := common.NewCredential(
		secretid,
		secretkey,
	)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	client, _ := dnspod.NewClient(credential, "", cpf)
	return client
}

func TencentDomainID(secretid, secretkey, SubDomain, RecordType, configDomain string) int {
	//获取域名列表
	requestList := dnspod.NewDescribeRecordListRequest()
	requestList.Domain = common.StringPtr(configDomain)
	responseList, err := TencentApiCommon(secretid, secretkey).DescribeRecordList(requestList)
	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		log.Printf("An API error has returned: %s", err)
		return 0
	}
	v := RecordList{}
	err = json.Unmarshal([]byte(responseList.ToJsonString()), &v)
	if err != nil {
		log.Println("json解析失败")
	}
	var recordID int
	for _, s := range v.Response.RecordList {
		if s.Name == SubDomain && s.Type == RecordType {
			recordID = s.RecordID
		}
	}
	return recordID
}

func TencentCreateRecord(configDomain, configSubdomain, RecordType, Value, secretid, secretkey string) {
	request := dnspod.NewCreateRecordRequest()

	request.Domain = common.StringPtr(configDomain)
	request.SubDomain = common.StringPtr(configSubdomain)
	request.RecordType = common.StringPtr(RecordType)
	request.RecordLine = common.StringPtr("默认")
	request.Value = common.StringPtr(Value)
	log.Printf("添加记录:%s.%s-%s", configSubdomain, configDomain, Value)

	response, err := TencentApiCommon(secretid, secretkey).CreateRecord(request)
	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		log.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		panic(err)
	}
	log.Printf("result:%s", response.ToJsonString())
}

func TencentDelRecord(configDomain, secretid, secretkey string, RecordId int) {
	request := dnspod.NewDeleteRecordRequest()
	request.Domain = common.StringPtr(configDomain)
	request.RecordId = common.Uint64Ptr(uint64(RecordId))
	log.Printf("删除记录：%s", strconv.Itoa(RecordId))

	response, err := TencentApiCommon(secretid, secretkey).DeleteRecord(request)
	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		log.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		panic(err)
	}
	log.Printf("result:%s", response.ToJsonString())
}

func TencentUpdateRecord(secretid, secretkey, SubDomain, RecordType, Status, Value, configDomain string, RecordId int) {
	//更新单个记录
	requestRecord := dnspod.NewModifyRecordRequest()
	requestRecord.Domain = common.StringPtr(configDomain)
	requestRecord.SubDomain = common.StringPtr(SubDomain)
	requestRecord.RecordType = common.StringPtr(RecordType)
	requestRecord.RecordLine = common.StringPtr("默认")
	requestRecord.RecordLineId = common.StringPtr("0")
	requestRecord.RecordId = common.Uint64Ptr(uint64(RecordId))
	requestRecord.Value = common.StringPtr(Value)
	requestRecord.Status = common.StringPtr(Status)
	log.Printf("更新记录：%s.%s-%s", SubDomain, configDomain, Value)

	responseRecord, err := TencentApiCommon(secretid, secretkey).ModifyRecord(requestRecord)
	if _, ok := err.(*errors.TencentCloudSDKError); ok {
		log.Printf("An API error has returned: %s", err)
		return
	}
	if err != nil {
		panic(err)
	}
	log.Printf("result:%s", responseRecord.ToJsonString())
}
