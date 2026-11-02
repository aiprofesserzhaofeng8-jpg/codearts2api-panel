import { createApp } from 'vue';
import App from './App.vue';
import UserConsole from './features/users/UserConsole.vue';
import './styles/console.css';
createApp(location.pathname === '/user' ? UserConsole : App).mount('#console-root');
