package database

import (
  "context"
  "sort"
  "sync"
  "log"
  orbitdb "berty.tech/go-orbit-db"
  "berty.tech/go-orbit-db/accesscontroller"
  "berty.tech/go-orbit-db/events"
  "berty.tech/go-orbit-db/iface"
  "berty.tech/go-orbit-db/stores"
  "berty.tech/go-orbit-db/stores/documentstore"
  config "github.com/ipfs/go-ipfs-config"
  "github.com/ipfs/go-ipfs/core"
  icore "github.com/ipfs/interface-go-ipfs-core"
  "github.com/libp2p/go-libp2p-core/crypto"
  "github.com/libp2p/go-libp2p-core/peer"
  "github.com/mitchellh/mapstructure"
  "go.uber.org/zap"

  "github.com/mrusme/superhighway84/cache"
  "github.com/mrusme/superhighway84/models"
)

type Database struct {
  ctx                 context.Context
  Offline             bool
  ConnectionString    string
  URI                 string
  CachePath           string
  Cache               *cache.Cache

  Logger              *zap.Logger

  IPFSNode            *core.IpfsNode
  IPFSCoreAPI         icore.CoreAPI

  OrbitDB             orbitdb.OrbitDB
  Store               orbitdb.DocumentStore
  StoreEventChan      <-chan events.Event
}

func (db *Database) init() (error) {
  var err error

  db.OrbitDB, err = orbitdb.NewOrbitDB(db.ctx, db.IPFSCoreAPI, &orbitdb.NewOrbitDBOptions{
    Directory: &db.CachePath,
    Logger: db.Logger,
  })
  if err != nil {
    log.Printf("1")
    return err
  }

  ac := &accesscontroller.CreateAccessControllerOptions{
    Access: map[string][]string{
      "write": {
        "*",
      },
    },
  }

  if err != nil {
    log.Printf("2")
    return err
  }

  // addr, err := db.OrbitDB.DetermineAddress(db.ctx, db.Name, "docstore", &orbitdb.DetermineAddressOptions{})
  // if err != nil {
  //   return err
  // }
  // db.URI = addr.String()

  storetype := "docstore"
  db.Store, err = db.OrbitDB.Docs(db.ctx, db.ConnectionString, &orbitdb.CreateDBOptions{
    LocalOnly: &db.Offline,
    AccessController: ac,
    StoreType: &storetype,
    StoreSpecificOpts: documentstore.DefaultStoreOptsForMap("id"),
  })
  if err != nil {
    log.Printf("3")
    return err
  }

  db.StoreEventChan = db.Store.Subscribe(db.ctx)
  return nil
}

func(db *Database) GetOwnID() string {
  return db.OrbitDB.Identity().ID
}

func(db *Database) GetOwnPubKey() crypto.PubKey {
  pubKey, err := db.OrbitDB.Identity().GetPublicKey()
  if err != nil {
    return nil
  }

  return pubKey
}

func(db *Database) connectToPeers() error {
  var wg sync.WaitGroup

  peerInfos, err := config.DefaultBootstrapPeers()
  if err != nil {
    return err
  }

  wg.Add(len(peerInfos))
  for _, peerInfo := range peerInfos {
    go func(peerInfo *peer.AddrInfo) {
      defer wg.Done()
      err := db.IPFSCoreAPI.Swarm().Connect(db.ctx, *peerInfo)
      if err != nil {
        db.Logger.Debug("failed to connect", zap.String("peerID", peerInfo.ID.String()), zap.Error(err))
      } else {
        db.Logger.Debug("connected!", zap.String("peerID", peerInfo.ID.String()))
      }
    }(&peerInfo)
  }
  wg.Wait()
  return nil
}

func NewDatabase(
  ctx context.Context,
  dbConnectionString string,
  dbCache string,
  cch *cache.Cache,
  logger *zap.Logger,
  offline bool,
) (*Database, error) {
  var err error

  db := new(Database)
  db.ctx = ctx
  db.ConnectionString = dbConnectionString
  db.CachePath = dbCache
  db.Cache = cch
  db.Logger = logger
  db.Offline = offline

  defaultPath, err := config.PathRoot()
  if err != nil {
    return nil, err
  }

  if err := setupPlugins(defaultPath); err != nil {
    return nil, err
  }

  db.IPFSNode, db.IPFSCoreAPI, err = createNode(ctx, defaultPath, offline)
  if err != nil {
    return nil, err
  }

  return db, nil
}

func (db *Database) Connect(onReady func(address string)) (error) {
  var err error

  // if db.Init {
    err = db.init()
    if err != nil {
      return err
    }
  // } else {
  //   err = db.open()
  //   if err != nil {
  //     return err
  //   }
  // }

  // go func() {
    err = db.connectToPeers()
    if err != nil {
      db.Logger.Debug("failed to connect: %s", zap.Error(err))
    } else {
      db.Logger.Debug("connected to peer!")
    }
  // }()

  // log.Println(db.Store.ReplicationStatus().GetBuffered())
  // log.Println(db.Store.ReplicationStatus().GetQueued())
  // log.Println(db.Store.ReplicationStatus().GetProgress())

  db.Logger.Info("running ...")

  go func() {
    for {
      for ev := range db.StoreEventChan {
        db.Logger.Debug("got event", zap.Any("event", ev))
        switch ev.(type) {
        case *stores.EventReady:
          db.URI = db.Store.Address().String()
          onReady(db.URI)
        }
      }
    }
  }()

  err = db.Store.Load(db.ctx, -1)
  if err != nil {
    // TODO: clean up
    return err
  }

  return nil
}

func (db *Database) Disconnect() {
  db.OrbitDB.Close()
}

func (db *Database) SubmitArticle(article *models.Article) (error) {
  entity, err := structToMap(*article)
  if err != nil {
    return err
  }
  entity["type"] = "article"

  _, err = db.Store.Put(db.ctx, entity)
  return err
}

func (db *Database) GetArticleByID(id string) (models.Article, error) {
  entity, err := db.Store.Get(db.ctx, id, &iface.DocumentStoreGetOptions{CaseInsensitive: false})
  if err != nil {
    return models.Article{}, err
  }

  var article models.Article
  err = mapstructure.Decode(entity[0], &article)
  if err != nil {
    return models.Article{}, err
  }

  return article, nil
}

func (db *Database) ListArticles() ([]*models.Article, []*models.Article, error) {
  var articles []*models.Article
  var articlesMap map[string]*models.Article

  articlesMap = make(map[string]*models.Article)

  _, err := db.Store.Query(db.ctx, func(e interface{})(bool, error) {
    entity := e.(map[string]interface{})
    if entity["type"] == "article" {
      var article models.Article
      err := mapstructure.Decode(entity, &article)
      if err == nil {
        // TODO: Not sure why mapstructure won't convert this field and simply
        //       leave it ""
        if entity["in-reply-to-id"] != nil {
          article.InReplyToID = entity["in-reply-to-id"].(string)
        }
        db.Cache.LoadArticle(&article)
        articles = append(articles, &article)
        articlesMap[article.ID] = articles[(len(articles) - 1)]
      }
      return true, err
    }
    return false, nil
  })
  if err != nil {
    return articles, nil, err
  }

  sort.SliceStable(articles, func(i, j int) bool {
    return articles[i].Date > articles[j].Date
  })

  var articlesRoots []*models.Article
  for i := 0; i < len(articles); i++ {
    if articles[i].InReplyToID != "" {
      inReplyTo := articles[i].InReplyToID
      if _, exist := articlesMap[inReplyTo]; exist == true {

        (*articlesMap[inReplyTo]).Replies =
          append((*articlesMap[inReplyTo]).Replies, articles[i])
        (*articlesMap[inReplyTo]).LatestReply = articles[i].Date
        continue
      }
    }
    articlesRoots = append(articlesRoots, articles[i])
  }

  sort.SliceStable(articlesRoots, func(i, j int) bool {
    iLatest := articlesRoots[i].LatestReply
    if iLatest <= 0 {
      iLatest = articlesRoots[i].Date
    }

    jLatest := articlesRoots[j].LatestReply
    if jLatest <= 0 {
      jLatest = articlesRoots[j].Date
    }

    return iLatest > jLatest
  })

  return articles, articlesRoots, nil
}
