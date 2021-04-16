package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	ocppj "github.com/lorenzodonini/ocpp-go/ocppj/ng"
	"github.com/lorenzodonini/ocpp-go/ocppj/ng/server"
	wsserver "github.com/lorenzodonini/ocpp-go/ws/ng/server"
)

const (
	port              = 9180
	path              = "/ocpp"
	requestBufferSize = 5
)

func main() {
	wsSrv := (&wsserver.Builder{}).WithSupportedSubprotocol(types.V16Subprotocol).Build()
	srv := server.New(wsSrv, core.Profile)
	conns, stop := srv.Start(port, fmt.Sprintf("%s/{id}", path))

	go func() {
		time.Sleep(30 * time.Second)
		stop <- struct{}{}
	}()

	fmt.Printf("streaming connections to %s:%d...\n", path, port)
	var wg sync.WaitGroup

	timeouts := wsserver.NewTimeoutConfig()
	for conn := range conns {
		if conn.Err != nil {
			fmt.Printf("got error receiving connection %v\n", conn.Err)
			continue
		}
		conn := conn.Connection.Start(requestBufferSize, timeouts.PingWait, timeouts.WriteWait)

		fmt.Printf("got conn from %s\n", conn.ClientID())

		readRequests, writeRequests := conn.Read(), conn.Write()

		wg.Add(1)
		go func() {
			defer wg.Done()

			boot := <-readRequests.Read
			if boot.Request().GetFeatureName() != core.BootNotificationFeatureName {
				fmt.Println("Expected boot notification")
				boot.Response() <- ocppj.NewResponseNotImplementedError("unexpected", nil)

				close(writeRequests.Write)
				close(readRequests.Drop)
				return
			}

			fmt.Println("Got boot notification")
			// but first change some config keys

			changeX, changeXConf, _ := ocppj.NewRequestPair(core.NewChangeConfigurationRequest("x", "y"))
			writeRequests.Write <- changeX
			fmt.Println("sent one")

			changeY, _, _ := ocppj.NewRequestPair(core.NewChangeConfigurationRequest("y", "z"))
			writeRequests.Write <- changeY
			fmt.Println("sent one")

			changeA, _, _ := ocppj.NewRequestPair(core.NewChangeConfigurationRequest("a", "b"))
			writeRequests.Write <- changeA
			fmt.Println("sent one")

			changeB, _, _ := ocppj.NewRequestPair(core.NewChangeConfigurationRequest("b", "c"))
			writeRequests.Write <- changeB
			fmt.Println("sent one")

			// Wait on confirmation
			fmt.Println("pre conf")

			select {
			case conf, ok := <-changeXConf:
				if !ok {
					fmt.Println("not getting a response I guess")
				} else {
					fmt.Println("Got confirmation to changeX", conf)
				}
			}
			fmt.Println("post conf")

			// Send boot notification confirmation
			response, _ := ocppj.NewResponseResult(
				&core.BootNotificationConfirmation{CurrentTime: types.NewDateTime(time.Now()), Status: core.RegistrationStatusAccepted},
			)
			// Make sure this runs
			ocppj.NewResponseError(
				fmt.Errorf("oh noes"),
			)
			boot.Response() <- response
			close(boot.Response())

			// Expect start/stop pairs only
			var transactionId = 0

			for req := range readRequests.Read {
				if req.Err() != nil {
					fmt.Printf("received error on conn %s\n", req.Err().Error())
					continue
				}
				switch req.Request().(type) {
				case *core.StartTransactionRequest:
					fmt.Println("started transaction")
					tagInfo := types.IdTagInfo{
						Status: types.AuthorizationStatusAccepted,
					}
					response, _ := ocppj.NewResponseResult(
						&core.StartTransactionConfirmation{IdTagInfo: &tagInfo, TransactionId: transactionId},
					)
					req.Response() <- response
					transactionId = transactionId + 1
				case *core.StopTransactionRequest:
					fmt.Println("stopped transaction")
					tagInfo := types.IdTagInfo{
						Status: types.AuthorizationStatusAccepted,
					}
					response, _ := ocppj.NewResponseResult(
						&core.StopTransactionConfirmation{IdTagInfo: &tagInfo},
					)
					req.Response() <- response
				default:
					fmt.Printf("unexpected request %v\n", req.Request())
				}
			}

			close(writeRequests.Write)
			fmt.Println("waiing conn closed")
			select {
			case <-writeRequests.Dropped:
				fmt.Println("connection closed")
			}
		}()
	}
	fmt.Println("waiting on existing conns")
	wg.Wait()
	fmt.Println("doing other things")
	time.Sleep(1 * time.Hour)
}
