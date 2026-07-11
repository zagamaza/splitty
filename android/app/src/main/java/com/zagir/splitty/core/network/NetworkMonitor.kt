package com.zagir.splitty.core.network

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Наблюдение за сетью: registerDefaultNetworkCallback → [isOnline].
 * «Онлайн» = у дефолтной сети есть INTERNET и она провалидирована
 * (VALIDATED — реально ходит в интернет, а не просто Wi-Fi без выхода).
 * Единственный источник правды для баннера «Офлайн», outbox и синка.
 */
@Singleton
class NetworkMonitor @Inject constructor(
    @ApplicationContext context: Context,
) {
    private val _isOnline: MutableStateFlow<Boolean>

    /** true — дефолтная сеть есть и провалидирована. */
    val isOnline: StateFlow<Boolean>

    init {
        val manager = context.getSystemService(ConnectivityManager::class.java)
        _isOnline = MutableStateFlow(manager?.isCurrentlyOnline() ?: false)
        isOnline = _isOnline.asStateFlow()

        manager?.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                // Дальше придёт onCapabilitiesChanged с VALIDATED; оптимистично true,
                // чтобы синк стартовал сразу после возвращения сети.
                _isOnline.value = true
            }

            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                _isOnline.value =
                    capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
                        capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
            }

            override fun onLost(network: Network) {
                _isOnline.value = false
            }

            override fun onUnavailable() {
                _isOnline.value = false
            }
        })
    }

    private fun ConnectivityManager.isCurrentlyOnline(): Boolean {
        val capabilities = getNetworkCapabilities(activeNetwork) ?: return false
        return capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
    }
}
