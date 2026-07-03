import { createRouter, createWebHistory } from 'vue-router'
import AdminView from '@/views/AdminView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'admin', component: AdminView },
  ],
})
