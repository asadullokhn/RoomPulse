import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import AdminView from '@/views/AdminView.vue'
import FloorView from '@/views/FloorView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/admin', name: 'admin', component: AdminView },
    { path: '/floor', name: 'floor', component: FloorView },
  ],
})
