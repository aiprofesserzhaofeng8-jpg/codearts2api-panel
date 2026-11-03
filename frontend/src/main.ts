import { createApp } from 'vue';
import './i18n';
import ConsoleApplication from './app/ConsoleApplication.vue';
import './styles/console.css';
import './styles/layout.css';
import './styles/user.css';
createApp(ConsoleApplication).mount('#console-root');
