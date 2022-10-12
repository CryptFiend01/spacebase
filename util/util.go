package util

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/wonderivan/logger"
)

func ReadFile(fileName string) []byte {
	f, err := os.OpenFile(fileName, os.O_RDONLY, 0600)
	if err != nil {
		logger.Error("Open file %s failed", fileName)
		return nil
	}

	defer f.Close()
	s, err := ioutil.ReadAll(f)
	if err != nil {
		logger.Error("Read file %s content error[%s]", fileName, err)
		return nil
	}

	return s
}

func RandomInt(n int) int {
	result, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(result.Int64())
}

func EthToWei(val float64) *big.Int {
	bigval := new(big.Float)
	bigval.SetFloat64(val)

	coin := new(big.Float)
	coin.SetInt(big.NewInt(1000000000000000000))

	bigval.Mul(bigval, coin)

	result := new(big.Int)
	bigval.Int(result) // store converted number in result

	return result
}

func WeiToEth(val *big.Int) float64 {
	fval := new(big.Float)
	fval.SetInt(val)

	coin := new(big.Float)
	coin.SetInt(big.NewInt(1000000000000000000))

	fval.Quo(fval, coin)

	f, _ := fval.Float64()
	return f
}

func Reserve(b []byte) []byte {
	n := len(b)
	for i := 0; i < n/2; i++ {
		b[i], b[n-i-1] = b[n-i-1], b[i]
	}
	return b
}

func Decrypt(str string) string {
	b := Reserve([]byte(str))
	dict := make(map[byte]int)
	origin := []byte("0123456789abcdef")
	for i, c := range origin {
		dict[c] = i
	}

	ret := []byte{}
	for i, c := range b {
		n := dict[c] - i%16
		if n < 0 {
			n += 16
		}
		ret = append(ret, origin[n])
	}
	return string(ret)
}

func MakeAuth(client *ethclient.Client, pKey string) *bind.TransactOpts {
	privateKey, err := crypto.HexToECDSA(pKey)
	if err != nil {
		fmt.Printf("HexToECDSA fail! error:%v\n", err)
		return nil
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		fmt.Println("Translate publicKey to ECDSA failed!")
		return nil
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		fmt.Printf("Pending nonce fail, err: %v\n", err)
		return nil
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		fmt.Printf("SuggestGasPrice fail, err: %v\n", err)
		return nil
	}
	fmt.Printf("GasPrice: %v\n", gasPrice.Int64())

	chainId, err := client.ChainID(context.Background())
	if err != nil {
		fmt.Printf("get ChainID fail, err: %v\n", err)
		return nil
	}
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainId)
	if err != nil {
		fmt.Printf("create auth by chain id fail, err: %v\n", err)
		return nil
	}
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(3000000)
	auth.GasPrice = gasPrice

	return auth
}

func WaitTxResult(client *ethclient.Client, wallet string, tx string) bool {
	for {
		time.Sleep(3 * time.Second)
		_, isPending, err := client.TransactionByHash(context.Background(), common.HexToHash(tx))
		if err != nil {
			logger.Error("query tx %s error: %v, give up tx.", tx, err)
			return false
		}

		if !isPending {
			receipt, err := client.TransactionReceipt(context.Background(), common.HexToHash(tx))
			if err != nil {
				logger.Error("query tx %s error: %v, give up tx.", tx, err)
				return false
			} else {
				if receipt.Status == 0 {
					logger.Debug("tx %s is failed, cancel record.", tx)
					return false
				} else {
					return true
				}
			}
		}
	}
}
