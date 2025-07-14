const path = require('path');
require('dotenv').config({ path: path.join(__dirname, '../.env') });
const { ethers } = require('ethers');
const fs = require('fs');

// 读取ABI
const abiPath = path.join(__dirname, '../abi/RewardToken.json');
const RewardTokenABI = JSON.parse(fs.readFileSync(abiPath, 'utf8'));

// 环境变量
const RPC_URL = process.env.BLOCKCHAIN_RPC_URL;
const PRIVATE_KEY = process.env.BLOCKCHAIN_PRIVATE_KEY_PARENT;
const REWARD_TOKEN_ADDRESS = process.env.TOKEN_CONTRACT_ADDRESS;

if (!RPC_URL || !PRIVATE_KEY || !REWARD_TOKEN_ADDRESS) {
  console.error('请在.env中配置BLOCKCHAIN_RPC_URL、BLOCKCHAIN_PRIVATE_KEY_PARENT和TOKEN_CONTRACT_ADDRESS');
  process.exit(1);
}

// 命令行参数
const [,, to, amount] = process.argv;
if (!to || !amount) {
  console.error('用法: node mint_fct.js <to_address> <amount>');
  process.exit(1);
}

async function main() {
  const provider = new ethers.JsonRpcProvider(RPC_URL);
  const wallet = new ethers.Wallet(PRIVATE_KEY, provider);
  const rewardToken = new ethers.Contract(REWARD_TOKEN_ADDRESS, RewardTokenABI, wallet);

  // 代币精度
  const decimals = await rewardToken.decimals();
  const mintAmount = ethers.parseUnits(amount, decimals);

  console.log(`准备为 ${to} 铸造 ${amount} FCT...`);
  const tx = await rewardToken.mint(to, mintAmount);
  console.log('交易已发送，hash:', tx.hash);
  const receipt = await tx.wait();
  console.log('交易已确认，区块号:', receipt.blockNumber);
}

main().catch(err => {
  console.error('执行失败:', err);
  process.exit(1);
}); 