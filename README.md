Add hogwarts service


# dependencies

```
go mod init github.com/Sathimantha/getVerification


go get github.com/joho/godotenv
go get github.com/gorilla/mux
go get github.com/go-sql-driver/mysql

```

## Adding service

```
sudo nano /etc/systemd/system/hogwarts.service
```

```
[Unit]
Description=Hogwarts Verification Web Server
After=network.target

[Service]
Type=simple
ExecStart=/home/bitnami/work/hogwarts_verify/getVerification
WorkingDirectory=/home/bitnami/work/hogwarts_verify
Restart=always
Environment="PATH=/usr/local/go/bin:/usr/bin:/bin"
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target

```

```
sudo chmod 644 /etc/systemd/system/hogwarts.service
sudo systemctl daemon-reload
sudo systemctl enable hogwarts.service
sudo systemctl start hogwarts.service
sudo systemctl status hogwarts.service
```