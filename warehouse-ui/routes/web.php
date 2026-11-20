<?php

use App\Http\Controllers\ApiProxyController;
use App\Http\Controllers\DashboardController;
use Illuminate\Support\Facades\Route;

Route::get('/', [DashboardController::class, 'index'])->name('dashboard');

// Jembatan browser -> backend Go (token API tetap di server)
Route::any('/proxy/{path}', ApiProxyController::class)
    ->where('path', '.*')
    ->name('proxy');
