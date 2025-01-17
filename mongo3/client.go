package mongo3

import (
    "gopkg.in/mgo.v2"
)

func Connect(url string) (*mgo.Session, error) {
    session, err := mgo.Dial(url)
    if (err != nil) {
        return nil, err
    }
    session.SetMode(mgo.Monotonic, true)
    return session, nil
}
