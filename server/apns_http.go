package server

import (
	"errors"
	"go-apns/apns"
	"go-apns/entry"
	"log"
	"net/http"
	"strconv"
	"sync"
)

type ApnsHttpServer struct {
	feedbackChan chan *entry.Feedback //用于接收feedback的chan
	apnsClient   *apns.ApnsClient
	pushId       uint32
	mutex        sync.Mutex
	expiredTime  uint32
	httpserver   *MomoHttpServer
}

func NewApnsHttpServer(option Option) *ApnsHttpServer {

	feedbackChan := make(chan *entry.Feedback, 1000)
	var apnsClient *apns.ApnsClient
	if option.startMode == STARTMODE_MOCK {
		//初始化mock apns
		apnsClient = apns.NewMockApnsClient(option.cert,
			option.pushAddr, chan<- *entry.Feedback(feedbackChan), option.feedbackAddr, entry.NewCycleLink(3, option.storageCapacity))
		log.Println("MOCK APNS HTTPSERVER IS STARTING ....")
	} else {
		//初始化apns
		apnsClient = apns.NewDefaultApnsClient(option.cert,
			option.pushAddr, chan<- *entry.Feedback(feedbackChan), option.feedbackAddr, entry.NewCycleLink(3, option.storageCapacity))
		log.Println("ONLINE APNS HTTPSERVER IS STARTING ....")
	}

	server := &ApnsHttpServer{feedbackChan: feedbackChan,
		apnsClient: apnsClient, expiredTime: option.expiredTime}

	//创建http
	server.httpserver = NewMomoHttpServer(option.bindAddr, nil)

	go server.dial(option.bindAddr)

	return server
}

func (self *ApnsHttpServer) dial(hp string) {

	log.Println("APNS HTTPSERVER IS STARTING ....")
	http.DefaultServeMux = http.NewServeMux()
	http.HandleFunc("/apns/push", self.handlePush)
	http.HandleFunc("/apns/feedback", self.handleFeedBack)
	err := self.httpserver.ListenAndServe()
	if nil != err {
		log.Printf("APNSHTTPSERVER|LISTEN|FAIL|%s\n", err)
	} else {
		log.Printf("APNSHTTPSERVER|LISTEN|SUCC|%s .....\n", hp)
	}

}

func (self *ApnsHttpServer) Shutdown() {
	self.httpserver.Shutdonw()
	self.apnsClient.Destory()
	log.Println("APNS HTTP SERVER SHUTDOWN SUCC ....")

}

func (self *ApnsHttpServer) handleFeedBack(out http.ResponseWriter, req *http.Request) {
	response := &response{}
	response.Status = RESP_STATUS_SUCC

	if req.Method == "GET" {
		//本次获取多少个feedback
		limitV := req.FormValue("limit")
		limit, _ := strconv.ParseInt(limitV, 10, 32)
		if limit > 100 {
			response.Status = RESP_STATUS_FETCH_FEEDBACK_OVER_LIMIT_ERROR
			response.Error = errors.New("Fetch Feedback Over limit 100 ")
		} else {
			//发起了获取feedback的请求
			err := self.apnsClient.FetchFeedback(int(limit))
			if nil != err {
				response.Error = err
				response.Status = RESP_STATUS_ERROR
			} else {
				//等待feedback数据
				packet := make([]*entry.Feedback, 0, limit)
				var feedback *entry.Feedback
				for ; limit > 0; limit-- {
					feedback = <-self.feedbackChan
					if nil == feedback {
						break
					}
					packet = append(packet, feedback)
				}
				response.Body = packet
			}
		}
	} else {
		response.Status = RESP_STATUS_INVALID_PROTO
		response.Error = errors.New("Unsupport Post method Invoke!")
	}

	self.write(out, response)
}

//处理push
func (self *ApnsHttpServer) handlePush(out http.ResponseWriter, req *http.Request) {

	resp := &response{}
	resp.Status = RESP_STATUS_SUCC
	if req.Method == "GET" {
		//返回不支持的请求方式
		resp.Status = RESP_STATUS_INVALID_PROTO
		resp.Error = errors.New("Unsupport Get method Invoke!")

	} else if req.Method == "POST" {

		//pushType
		pushType := req.PostFormValue("pt") //先默认采用Enhanced方式
		//接卸对应的token和payload
		token, payload := self.decodePayload(req, resp)

		//----------------如果依然是成功状态则证明当前可以发送
		if RESP_STATUS_SUCC == resp.Status {
			self.innerSend(pushType, token, payload, resp)
		}

	}
	self.write(out, resp)
}

func (self *ApnsHttpServer) write(out http.ResponseWriter, resp *response) {
	out.Header().Set("content-type", "text/json")
	out.Write(resp.Marshal())
}
