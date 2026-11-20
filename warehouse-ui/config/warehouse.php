<?php

return [
    // Alamat backend Go (lihat PORT di backend-go, default 8080)
    'api_url'   => rtrim(env('WAREHOUSE_API_URL', 'http://127.0.0.1:8080'), '/'),

    // Harus sama dengan API_TOKEN di backend Go. Token TIDAK pernah dikirim ke browser.
    'api_token' => env('WAREHOUSE_API_TOKEN', 'smart-warehouse-secret-key-2026'),

    'timeout'   => (int) env('WAREHOUSE_API_TIMEOUT', 8),

    'device'    => env('WAREHOUSE_DEVICE_NAME', 'SMART-WH-LIFT-01'),
];
