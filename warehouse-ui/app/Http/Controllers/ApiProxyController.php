<?php

namespace App\Http\Controllers;

use App\Services\WarehouseApi;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;

/**
 * Meneruskan /proxy/{path} dari browser ke Go /api/v1/{path}.
 * Hanya path yang dikenal yang boleh lewat.
 */
class ApiProxyController extends Controller
{
    private const ALLOWED = [
        '#^lift/(status|telemetry|stop|reset|enable|disable|mode|move|jog|home)$#',
        '#^commands(/[\w\-]+)?$#',
        '#^items(/\d+)?$#',
        '#^items/\d+/units$#',
        '#^units/[^/]+$#',
        '#^stock/(summary|levels|adjust)$#',
        '#^movements$#',
        '#^alarms$#',
        '#^alarms/\d+/ack$#',
        '#^settings$#',
    ];

    public function __invoke(Request $request, WarehouseApi $api, string $path): JsonResponse
    {
        $path = trim($path, '/');

        $ok = false;
        foreach (self::ALLOWED as $re) {
            if (preg_match($re, $path)) { $ok = true; break; }
        }
        if (!$ok) {
            return response()->json(['data' => null, 'error' => ['code' => 'NOT_ALLOWED', 'message' => 'Endpoint tidak diizinkan']], 404);
        }

        $json = in_array($request->method(), ['POST', 'PUT', 'PATCH'], true) ? $request->json()->all() : null;

        if (is_array($json) && !isset($json['requested_by'])) {
            $json['requested_by'] = 'Operator Dashboard';
        }

        [$status, $body] = $api->forward($request->method(), $path, $request->query(), $json);
        return response()->json($body, $status);
    }
}
