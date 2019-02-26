package constantpos

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ninjadotorg/constant/blockchain"
	"github.com/ninjadotorg/constant/cashec"
	"github.com/ninjadotorg/constant/common"
	"github.com/ninjadotorg/constant/wire"
)

type Engine struct {
	sync.Mutex
	started bool

	// channel
	cQuit   chan struct{}
	cBFTMsg chan wire.Message

	config EngineConfig
}

type EngineConfig struct {
	BlockChain  *blockchain.BlockChain
	ChainParams *blockchain.Params
	BlockGen    *blockchain.BlkTmplGenerator
	UserKeySet  *cashec.KeySet
	NodeMode    string
	Server      serverInterface
}

//Init apply configuration to consensus engine
func (engine Engine) Init(cfg *EngineConfig) (*Engine, error) {
	return &Engine{
		config: *cfg,
	}, nil
}

func (engine *Engine) Start() error {
	engine.Lock()
	defer engine.Unlock()
	if engine.started {
		return errors.New("Consensus engine is already started")
	}
	engine.cQuit = make(chan struct{})
	engine.cBFTMsg = make(chan wire.Message)
	engine.started = true
	Logger.log.Info("Start consensus with key", engine.config.UserKeySet.GetPublicKeyB58())
	fmt.Println(engine.config.BlockChain.BestState.Beacon.BeaconCommittee)

	//Note: why goroutine in this function
	go func() {
		for {
			select {
			case <-engine.cQuit:
				return
			default:
				if engine.config.BlockChain.IsReady(false, 0) {
					userRole, shardID := engine.config.BlockChain.BestState.Beacon.GetPubkeyRole(engine.config.UserKeySet.GetPublicKeyB58())
					nodeRole := common.EmptyString
					if (engine.config.NodeMode == common.NODEMODE_BEACON || engine.config.NodeMode == common.NODEMODE_AUTO) && userRole != common.SHARD_ROLE && userRole != common.EmptyString {
						nodeRole = common.BEACON_ROLE
					}
					if (engine.config.NodeMode == common.NODEMODE_SHARD || engine.config.NodeMode == common.NODEMODE_AUTO) && userRole == common.SHARD_ROLE {
						nodeRole = common.SHARD_ROLE
					}
					go engine.config.Server.UpdateConsensusState(nodeRole, engine.config.UserKeySet.GetPublicKeyB58(), nil, engine.config.BlockChain.BestState.Beacon.BeaconCommittee, engine.config.BlockChain.BestState.Beacon.ShardCommittee)
					time.Sleep(2 * time.Second)
					fmt.Println(engine.config.NodeMode, userRole, shardID)
					if userRole != common.EmptyString {
						bftProtocol := &BFTProtocol{
							cQuit:      engine.cQuit,
							cBFTMsg:    engine.cBFTMsg,
							BlockGen:   engine.config.BlockGen,
							UserKeySet: engine.config.UserKeySet,
							Chain:      engine.config.BlockChain,
							Server:     engine.config.Server,
						}
						if (engine.config.NodeMode == common.NODEMODE_BEACON || engine.config.NodeMode == common.NODEMODE_AUTO) && userRole != common.SHARD_ROLE {
							bftProtocol.RoleData.Committee = make([]string, len(engine.config.BlockChain.BestState.Beacon.BeaconCommittee))
							copy(bftProtocol.RoleData.Committee, engine.config.BlockChain.BestState.Beacon.BeaconCommittee)
							var (
								err    error
								resBlk interface{}
							)
							switch userRole {
							case common.PROPOSER_ROLE:
								resBlk, err = bftProtocol.Start(true, common.BEACON_ROLE, 0)
								if err != nil {
									Logger.log.Error("PBFT fatal error", err)
									continue
								}
							case common.VALIDATOR_ROLE:
								msgReady, _ := MakeMsgBFTReady(engine.config.BlockChain.BestState.Beacon.Hash())
								engine.config.Server.PushMessageToBeacon(msgReady)
								resBlk, err = bftProtocol.Start(false, common.BEACON_ROLE, 0)
								if err != nil {
									Logger.log.Error("PBFT fatal error", err)
									continue
								}
							default:
								err = errors.New("Not your turn yet")
							}

							if err == nil {
								fmt.Println(resBlk.(*blockchain.BeaconBlock))
								err = engine.config.BlockChain.InsertBeaconBlock(resBlk.(*blockchain.BeaconBlock), false)
								if err != nil {
									Logger.log.Error("Insert beacon block error", err)
									continue
								}
								//PUSH BEACON TO ALL
								newBeaconBlock := resBlk.(*blockchain.BeaconBlock)
								newBeaconBlockMsg, err := MakeMsgBeaconBlock(newBeaconBlock)
								if err != nil {
									Logger.log.Error("Make new beacon block message error", err)
								} else {
									engine.config.Server.PushMessageToAll(newBeaconBlockMsg)
								}

							} else {
								Logger.log.Error(err)
							}
							continue
						}
						if (engine.config.NodeMode == common.NODEMODE_SHARD || engine.config.NodeMode == common.NODEMODE_AUTO) && userRole == common.SHARD_ROLE {
							engine.config.BlockChain.SyncShard(shardID)
							engine.config.BlockChain.StopSyncUnnecessaryShard()
							bftProtocol.RoleData.Committee = make([]string, len(engine.config.BlockChain.BestState.Shard[shardID].ShardCommittee))
							copy(bftProtocol.RoleData.Committee, engine.config.BlockChain.BestState.Shard[shardID].ShardCommittee)
							var (
								err    error
								resBlk interface{}
							)
							if engine.config.BlockChain.IsReady(true, shardID) {
								shardRole := engine.config.BlockChain.BestState.Shard[shardID].GetPubkeyRole(engine.config.UserKeySet.GetPublicKeyB58())
								fmt.Println("My shard role", shardRole)
								switch shardRole {
								case common.PROPOSER_ROLE:
									resBlk, err = bftProtocol.Start(true, common.SHARD_ROLE, shardID)
									if err != nil {
										Logger.log.Error("PBFT fatal error", err)
										continue
									}
								case common.VALIDATOR_ROLE:
									msgReady, _ := MakeMsgBFTReady(engine.config.BlockChain.BestState.Shard[shardID].Hash())
									engine.config.Server.PushMessageToShard(msgReady, shardID)
									resBlk, err = bftProtocol.Start(false, common.SHARD_ROLE, shardID)
									if err != nil {
										Logger.log.Error("PBFT fatal error", err)
										continue
									}
								default:
									err = errors.New("Not your turn yet")
								}
								if err == nil {
									fmt.Println("========NEW SHARD BLOCK=======", resBlk.(*blockchain.ShardBlock).Header.Height)
									err = engine.config.BlockChain.InsertShardBlock(resBlk.(*blockchain.ShardBlock))
									if err != nil {
										Logger.log.Error("Insert shard block error", err)
										continue
									}
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 1")
									//PUSH SHARD TO BEACON
									shardBlk, ok := resBlk.(*blockchain.ShardBlock)
									if !ok {
										Logger.log.Debug("Got data of type %T but wanted blockchain.ShardBlock", resBlk)
										continue
									}
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 2")
									newShardToBeaconBlock := shardBlk.CreateShardToBeaconBlock(engine.config.BlockChain)
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 3")
									newShardToBeaconMsg, err := MakeMsgShardToBeaconBlock(newShardToBeaconBlock)
									if err == nil {
										go engine.config.Server.PushMessageToBeacon(newShardToBeaconMsg)
									}
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 4")
									//PUSH CROSS-SHARD
									newCrossShardBlocks := shardBlk.CreateAllCrossShardBlock(engine.config.BlockChain.BestState.Beacon.ActiveShards)
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 5")
									for sID, newCrossShardBlock := range newCrossShardBlocks {
										newCrossShardMsg, err := MakeMsgCrossShardBlock(newCrossShardBlock)
										if err == nil {
											engine.config.Server.PushMessageToShard(newCrossShardMsg, sID)
										}
									}
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 6")
									if err != nil {
										Logger.log.Error("Make new block message error", err)
									}
									fmt.Println(">>>>>>>>>>>>>+++++++++++++++<<<<<<<< 7")
								} else {
									Logger.log.Error(err)
								}
							} else {
								Logger.log.Warn("Blockchain is not ready!")
							}
						}
					} else {
						time.Sleep(5 * time.Second)
					}
				}
			}
		}
	}()

	return nil
}

func (engine *Engine) Stop() error {
	engine.Lock()
	defer engine.Unlock()
	if !engine.started {
		return errors.New("Consensus engine is already stopped")
	}

	engine.started = false
	close(engine.cQuit)
	return nil
}