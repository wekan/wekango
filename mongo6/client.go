package mongo6

import (
    "context"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

func Connect(url string) (*mongo.Client, error) {
    clientOptions := options.Client().ApplyURI(url)
    client, err := mongo.Connect(context.Background(), clientOptions)
    if err != nil {
        return nil, err
    }
    
    if err = client.Ping(context.Background(), nil); err != nil {
        return nil, err
    }
    
    return client, nil
}
