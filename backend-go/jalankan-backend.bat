@echo off
rem Jalankan dari folder backend-go (satu folder dengan server.exe)
rem Sesuaikan DB_PASSWORD jika MySQL Anda memakai password.
set PORT=8080
set API_TOKEN=smart-warehouse-secret-key-2026
set DB_USER=root
set DB_PASSWORD=
set DB_HOST=127.0.0.1
set DB_PORT=3306
set DB_NAME=smart_warehouse
set ENABLE_SIMULATOR=true
server.exe
pause
