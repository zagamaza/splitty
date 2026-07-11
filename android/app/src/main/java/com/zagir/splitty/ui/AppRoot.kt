package com.zagir.splitty.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.core.session.Session
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.ui.auth.LoginScreen
import com.zagir.splitty.ui.main.MainScaffold
import com.zagir.splitty.ui.theme.Splitty
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.StateFlow

/** Корневой VM: единственная задача — отдать состояние сессии в AppRoot. */
@HiltViewModel
class AppRootViewModel @Inject constructor(
    sessionStore: SessionStore,
) : ViewModel() {
    val session: StateFlow<Session?> = sessionStore.state
}

/**
 * Корень приложения: DataStore ещё читается → пустой фон (без мигания
 * логина), нет токена → LoginScreen, есть → MainScaffold.
 * Аналог iOS RootView.
 */
@Composable
fun AppRoot(viewModel: AppRootViewModel = hiltViewModel()) {
    val session by viewModel.session.collectAsStateWithLifecycle()
    Surface(modifier = Modifier.fillMaxSize(), color = Splitty.colors.bg) {
        when {
            session == null -> Box(
                Modifier
                    .fillMaxSize()
                    .background(Splitty.colors.bg)
            )

            session?.isAuthenticated == true -> MainScaffold()

            else -> LoginScreen()
        }
    }
}
