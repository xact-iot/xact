import { connect } from 'nats.ws';

async function testConnection() {
  try {
    console.log('Attempting to connect to ws://localhost:9222...');
    const nc = await connect({ 
      servers: 'ws://localhost:9222',
      timeout: 10000 
    });
    console.log('✓ Connected to NATS WebSocket server');
    
    const js = nc.jetstream();
    const kv = await js.views.kv('rtdb');
    console.log('✓ Accessed KV bucket "rtdb"');
    
    const entry = await kv.get('building.floor1.temperature');
    if (entry) {
      const value = JSON.parse(new TextDecoder().decode(entry.value));
      console.log(`✓ Got value from KV: ${value}`);
    } else {
      console.log('⚠ No value found in KV (may be normal if backend just started)');
    }
    
    await nc.close();
    console.log('✓ Disconnected');
    console.log('\n✅ All connection tests passed!');
    process.exit(0);
  } catch (error) {
    console.error('❌ Connection test failed:', error.message);
    process.exit(1);
  }
}

testConnection();
