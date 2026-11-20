<?php

namespace App\Services;

use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Facades\Http;

/**
 * Klien tipis ke REST API Go (/api/v1/*).
 * Mengembalikan [status, body] dengan body berbentuk {data, error} seperti backend.
 */
class WarehouseApi
{
    public function forward(string $method, string $path, array $query = [], ?array $json = null): array
    {
        $url = config('warehouse.api_url') . '/api/v1/' . ltrim($path, '/');

        try {
            $req = Http::withToken(config('warehouse.api_token'))
                ->acceptJson()
                ->timeout(config('warehouse.timeout'));

            $payload = $json ?? new \stdClass();

            $res = match (strtoupper($method)) {
                'GET'    => $req->get($url, $query),
                'POST'   => $req->post($url, $payload),
                'PUT'    => $req->put($url, $payload),
                'PATCH'  => $req->patch($url, $payload),
                default  => throw new \InvalidArgumentException('Method tidak didukung'),
            };

            $body = $res->json();
            if (!is_array($body)) {
                $body = ['data' => null, 'error' => ['code' => 'BAD_GATEWAY', 'message' => 'Respons backend tidak valid']];
            }
            return [$res->status(), $body];
        } catch (ConnectionException $e) {
            return [502, ['data' => null, 'error' => [
                'code'    => 'BACKEND_DOWN',
                'message' => 'Backend Go tidak dapat dihubungi di ' . config('warehouse.api_url') . '. Pastikan server.exe berjalan.',
            ]]];
        }
    }
}
