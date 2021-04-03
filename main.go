package main

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	eth               *ethclient.Client
	geth              *GethInfo
	delay             int
	watchingAddresses string
	taskPrefix				string
	addresses         map[string]Address
)

func init() {
	geth = new(GethInfo)
	addresses = make(map[string]Address)
	geth.TotalEth = big.NewInt(0)
}

type GethInfo struct {
	GethServer       string
	ContractsCreated int64
	TokenTransfers   int64
	ContractCalls    int64
	EthTransfers     int64
	BlockSize        float64
	LoadTime         float64
	TotalEth         *big.Int
	CurrentBlock     *types.Block
	Sync             *ethereum.SyncProgress
	LastBlockUpdate  time.Time
	SugGasPrice      *big.Int
	PendingTx        uint
	NetworkId        *big.Int
}

type Address struct {
	Balance *big.Int
	Address string
	Nonce   uint64
}

func main() {
	var err error
	defer eth.Close()
	geth.GethServer = os.Getenv("GETH")
	watchingAddresses = os.Getenv("ADDRESSES")
	
	taskPrefix = os.Getenv("PREFIX")
	if len(taskPrefix) == 0 {
		taskPrefix = "geth-node"	
	}
	
	delay, _ = strconv.Atoi(os.Getenv("DELAY"))
	if delay == 0 {
		delay = 500
	}
	log.Printf("Connecting to Ethereum node: %v\n", geth.GethServer)
	eth, err = ethclient.Dial(geth.GethServer)
	if err != nil {
		panic(err)
	}
	geth.CurrentBlock, err = eth.BlockByNumber(context.TODO(), nil)
	if err != nil {
		panic(err)
	}

	go Routine()

	log.Printf("Geth Exporter running on http://localhost:9090/metrics\n")

	http.HandleFunc("/metrics", MetricsHttp)
	err = http.ListenAndServe(":9090", nil)
	if err != nil {
		panic(err)
	}
}

func CalculateTotals(block *types.Block) {
	geth.TotalEth = big.NewInt(0)
	geth.ContractsCreated = 0
	geth.TokenTransfers = 0
	geth.EthTransfers = 0
	for _, b := range block.Transactions() {

		if b.To() == nil {
			geth.ContractsCreated++
		}

		if len(b.Data()) >= 4 {
			method := hexutil.Encode(b.Data()[:4])
			if method == "0xa9059cbb" {
				geth.TokenTransfers++
			}
		}

		if b.Value().Sign() == 1 {
			geth.EthTransfers++
		}

		geth.TotalEth.Add(geth.TotalEth, b.Value())
	}

	size := strings.Split(geth.CurrentBlock.Size().String(), " ")
	geth.BlockSize = stringToFloat(size[0]) * 1000
}

func Routine() {
	var lastBlock *types.Block
	ctx := context.Background()
	for {
		t1 := time.Now()
		var err error
		geth.CurrentBlock, err = eth.BlockByNumber(ctx, nil)
		if err != nil {
			log.Printf("issue with response from geth server: %v\n", geth.CurrentBlock)
			time.Sleep(time.Duration(delay) * time.Millisecond)
			continue
		}
		geth.SugGasPrice, _ = eth.SuggestGasPrice(ctx)
		geth.PendingTx, _ = eth.PendingTransactionCount(ctx)
		geth.NetworkId, _ = eth.NetworkID(ctx)
		geth.Sync, _ = eth.SyncProgress(ctx)

		if lastBlock == nil || geth.CurrentBlock.NumberU64() > lastBlock.NumberU64() {
			log.Printf("Received block #%v with %v transactions (%v)\n", geth.CurrentBlock.NumberU64(), len(geth.CurrentBlock.Transactions()), geth.CurrentBlock.Hash().String())
			geth.LastBlockUpdate = time.Now()
			geth.LoadTime = time.Now().Sub(t1).Seconds()
		}

		if watchingAddresses != "" {
			for _, a := range strings.Split(watchingAddresses, ",") {
				addr := common.HexToAddress(a)
				balance, _ := eth.BalanceAt(ctx, addr, geth.CurrentBlock.Number())
				nonce, _ := eth.NonceAt(ctx, addr, geth.CurrentBlock.Number())
				address := Address{
					Address: addr.String(),
					Balance: balance,
					Nonce:   nonce,
				}
				addresses[a] = address
			}
		}

		lastBlock = geth.CurrentBlock
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

//
// HTTP response handler for /metrics
func MetricsHttp(w http.ResponseWriter, r *http.Request) {
	var allOut []string
	block := geth.CurrentBlock
	if block == nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("issue receiving block from URL: %v", geth.GethServer)))
		return
	}
	CalculateTotals(block)

	allOut = append(allOut, fmt.Sprintf("%v_eth_block %v",taskPrefix, block.NumberU64()))
	allOut = append(allOut, fmt.Sprintf("%v_eth_seconds_last_block %0.2f",taskPrefix, time.Now().Sub(geth.LastBlockUpdate).Seconds()))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_transactions %v",taskPrefix, len(block.Transactions())))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_value %v",taskPrefix, ToEther(geth.TotalEth)))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_gas_used %v",taskPrefix, block.GasUsed()))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_gas_limit %v",taskPrefix, block.GasLimit()))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_nonce %v",taskPrefix, block.Nonce()))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_difficulty %v",taskPrefix, block.Difficulty()))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_uncles %v",taskPrefix, len(block.Uncles())))
	allOut = append(allOut, fmt.Sprintf("%v_eth_block_size_bytes %v",taskPrefix, geth.BlockSize))
	allOut = append(allOut, fmt.Sprintf("%v_eth_gas_price %v",taskPrefix, geth.SugGasPrice))
	allOut = append(allOut, fmt.Sprintf("%v_eth_pending_transactions %v",taskPrefix, geth.PendingTx))
	allOut = append(allOut, fmt.Sprintf("%v_eth_network_id %v",taskPrefix, geth.NetworkId))
	allOut = append(allOut, fmt.Sprintf("%v_eth_contracts_created %v",taskPrefix, geth.ContractsCreated))
	allOut = append(allOut, fmt.Sprintf("%v_eth_token_transfers %v",taskPrefix, geth.TokenTransfers))
	allOut = append(allOut, fmt.Sprintf("%v_eth_eth_transfers %v",taskPrefix, geth.EthTransfers))
	allOut = append(allOut, fmt.Sprintf("%v_eth_load_time %0.4f",taskPrefix, geth.LoadTime))

	if geth.Sync != nil {
		allOut = append(allOut, fmt.Sprintf("%v_eth_known_states %v",taskPrefix, int(geth.Sync.KnownStates)))
		allOut = append(allOut, fmt.Sprintf("%v_eth_highest_block %v",taskPrefix, int(geth.Sync.HighestBlock)))
		allOut = append(allOut, fmt.Sprintf("%v_eth_pulled_states %v",taskPrefix, int(geth.Sync.PulledStates)))
	}

	for _, v := range addresses {
		allOut = append(allOut, fmt.Sprintf("%v_eth_address_balance{address=\"%v\"} %v",taskPrefix,, v.Address, ToEther(v.Balance).String()))
		allOut = append(allOut, fmt.Sprintf("%v_eth_address_nonce{address=\"%v\"} %v",taskPrefix,, v.Address, v.Nonce))
	}

	w.Write([]byte(strings.Join(allOut, "\n")))
}

// stringToFloat will simply convert a string to a float
func stringToFloat(s string) float64 {
	amount, _ := strconv.ParseFloat(s, 10)
	return amount
}

//
// CONVERTS WEI TO ETH
func ToEther(o *big.Int) *big.Float {
	pul, int := big.NewFloat(0), big.NewFloat(0)
	int.SetInt(o)
	pul.Mul(big.NewFloat(0.000000000000000001), int)
	return pul
}
