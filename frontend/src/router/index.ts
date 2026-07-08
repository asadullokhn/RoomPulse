import { createRouter, createWebHashHistory } from 'vue-router'
import AdminLayout from '@/layouts/AdminLayout.vue'
import LoginView from '@/views/LoginView.vue'
import { getToken } from '@/api/auth'

const router = createRouter({
  // Hash history: the backend owns GET /reservations, /rooms, /users... for
  // the mobile API, so path-based deep links would collide with JSON routes.
  history: createWebHashHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView },
    {
      path: '/',
      component: AdminLayout,
      children: [
        { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'reservations', name: 'reservations', component: () => import('@/views/ReservationsView.vue') },
        { path: 'rooms', name: 'rooms', component: () => import('@/views/RoomsView.vue') },
        { path: 'rooms/:ws', name: 'room-detail', component: () => import('@/views/RoomDetailView.vue') },
        { path: 'users', name: 'users', component: () => import('@/views/UsersView.vue') },
        { path: 'users/:id', name: 'user-detail', component: () => import('@/views/UserDetailView.vue') },
        { path: 'notifications', name: 'notifications', component: () => import('@/views/NotificationsView.vue') },
      ],
    },
  ],
})

router.beforeEach((to) => {
  if (to.name !== 'login' && !getToken()) return { name: 'login' }
  if (to.name === 'login' && getToken()) return { name: 'dashboard' }
})

export default router
