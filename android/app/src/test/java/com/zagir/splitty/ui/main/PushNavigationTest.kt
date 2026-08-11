package com.zagir.splitty.ui.main

import android.content.Context
import android.content.Intent
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.ComposeNavigator
import androidx.navigation.compose.composable
import androidx.navigation.createGraph
import androidx.navigation.navArgument
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.push.PushRoute
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Тап по push доезжает до НАВИГАЦИИ.
 *
 * До этого extras уведомления не читал никто (`getStringExtra` не встречался в
 * приложении вовсе), и тап просто открывал приложение там, где его оставили.
 * Поэтому проверяется не «функция вернула строку», а настоящий `NavController`
 * на тех же самых константах маршрутов, что регистрирует `MainNavHost`: адрес,
 * разошедшийся с шаблоном, уронит тест здесь, а не у пользователя.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PushNavigationTest {

    private lateinit var nav: NavHostController

    @BeforeTest
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        nav = NavHostController(context).apply {
            navigatorProvider.addNavigator(ComposeNavigator())
            graph = createGraph(startDestination = MainRoutes.FRIENDS) {
                composable(MainRoutes.FRIENDS) {}
                composable(MainRoutes.ACTIVITY) {}
                composable(
                    MainRoutes.ROOM,
                    arguments = listOf(navArgument("roomId") { type = NavType.StringType }),
                ) {}
                composable(
                    MainRoutes.OPERATION,
                    arguments = listOf(
                        navArgument("roomId") { type = NavType.StringType },
                        navArgument("operationId") { type = NavType.StringType },
                    ),
                ) {}
            }
        }
    }

    private val currentRoute: String? get() = nav.currentBackStackEntry?.destination?.route

    private fun arg(name: String): String? =
        nav.currentBackStackEntry?.arguments?.getString(name)

    private fun countOf(route: String): Int =
        nav.currentBackStack.value.count { it.destination.route == route }

    @Test
    fun `expense push opens the operation card above its room`() {
        nav.openPushRoute(PushRoute.Operation("room-1", "op-1"))

        assertEquals(MainRoutes.OPERATION, currentRoute)
        assertEquals("room-1", arg("roomId"))
        assertEquals("op-1", arg("operationId"))
        // Комната под карточкой обязательна: «назад» с удалённой операции
        // иначе выбрасывало бы человека из группы, о которой был пуш.
        assertEquals(1, countOf(MainRoutes.ROOM))
    }

    @Test
    fun `debt push opens the room itself`() {
        nav.openPushRoute(PushRoute.Room("room-1"))

        assertEquals(MainRoutes.ROOM, currentRoute)
        assertEquals("room-1", arg("roomId"))
    }

    @Test
    fun `invite push opens the notifications tab, never the room`() {
        nav.openPushRoute(PushRoute.Notifications)

        assertEquals(MainRoutes.ACTIVITY, currentRoute)
        assertEquals(0, countOf(MainRoutes.ROOM))
    }

    /** Повторный тап по тому же уведомлению не должен плодить экраны. */
    @Test
    fun `repeated tap does not stack duplicates`() {
        repeat(3) { nav.openPushRoute(PushRoute.Operation("room-1", "op-1")) }

        assertEquals(1, countOf(MainRoutes.ROOM))
        assertEquals(1, countOf(MainRoutes.OPERATION))
        assertEquals(MainRoutes.OPERATION, currentRoute)
    }

    /** Пуш о долге, пришедший, когда человек и так внутри этой комнаты. */
    @Test
    fun `room push while inside that room changes nothing`() {
        nav.openPushRoute(PushRoute.Operation("room-1", "op-1"))
        nav.openPushRoute(PushRoute.Room("room-1"))

        assertEquals(1, countOf(MainRoutes.ROOM))
        assertEquals(MainRoutes.OPERATION, currentRoute)
    }

    /** Другая операция той же комнаты — второй комнаты в стеке не появляется. */
    @Test
    fun `another operation of the same room reuses the room screen`() {
        nav.openPushRoute(PushRoute.Operation("room-1", "op-1"))
        nav.openPushRoute(PushRoute.Operation("room-1", "op-2"))

        assertEquals(1, countOf(MainRoutes.ROOM))
        assertEquals("op-2", arg("operationId"))
    }

    /** Сквозной путь: extras интента → маршрут → экран. */
    @Test
    fun `intent extras from the notification reach navigation`() {
        val intent = Intent()
            .putExtra("roomId", "68f2a1c4d9")
            .putExtra("operationId", "6a7b339f63a4ee45a2aed6db")
            .putExtra("type", "operation")

        nav.openPushRoute(requireNotNull(PushRoute.fromIntent(intent)))

        assertEquals(MainRoutes.OPERATION, currentRoute)
        assertEquals("68f2a1c4d9", arg("roomId"))
        assertEquals("6a7b339f63a4ee45a2aed6db", arg("operationId"))
    }
}
