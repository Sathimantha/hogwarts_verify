cp .env .env.bkp
sudo systemctl stop hogwarts.service
git pull
rm .env
mv .env.bkp .env
go build
sudo systemctl start hogwarts.service
